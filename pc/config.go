// Copyright 2026 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

// Package pc implements Harness Cloud Private Connectivity lifecycle for lite-engine.
//
// Canonical /setup order:
//  1. Decode SetupRequest.
//  2. ExtractAndStrip — extract the one-time enrollment ticket, mask it, and strip all fields.
//  3. Validate capability; fail closed if PC enabled but tailscale not present.
//  4. Mutual exclusion: PC + egress → fail.
//  5. Repair Linux arm64 clock drift before consuming the one-time ticket.
//  6. Exchange the ticket immediately before joining with WIF (file-backed JWT,
//     never argv or os.Setenv).
//  7. engine.Setup applies container-scoped DNS/MTU configuration.
//
// /destroy order:
//  1. engine.Destroy (containers first while connectivity remains).
//  2. Logout from the prebaked Tailscale runtime.
//  3. Delete token/marker files.
package pc

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// On-VM env var names — FROZEN. Do NOT change without a coordinated update
// to ci-manager VmInitializeTaskParamsBuilder and the InternalApi contract.
const (
	EnvEnabled          = "HARNESS_PC_ENABLED"
	EnvEnrollmentTicket = "HARNESS_PC_ENROLLMENT_TICKET"

	DefaultTag  = "tag:ci-runner"
	JoinTimeout = 90 * time.Second

	maxOIDCTokenLifetime = 15 * time.Minute
	oidcClockSkew        = 30 * time.Second
	jwtSegmentCount      = 3
)

// Config holds the private connectivity configuration extracted from HARNESS_PC_* env vars.
// All fields are extracted and the source envs are stripped before any further processing.
type Config struct {
	Enabled bool

	// EnrollmentTicket is the only secret accepted from the initial setup
	// request. It is exchanged once, immediately before Tailscale join.
	EnrollmentTicket string
	contractPresent  bool
	unexpectedField  bool

	// The remaining fields are server-authoritative exchange results.
	ClientID          string
	OIDCToken         string
	Hostname          string
	Tag               string
	BindingGeneration uint64
	ExpiresAt         time.Time
}

// ExtractAndStrip reads all HARNESS_PC_* values from envs and removes every key.
//
// MUST be called immediately after decoding SetupRequest, before setProxyEnvs, setHarnessEnvs,
// state.Set, cfg construction, or any logging of envs.
//
// The returned Config.EnrollmentTicket is secret and must be added to the secrets masking list
// before calling state.Set(s.Secrets, ...).
func ExtractAndStrip(envs map[string]string) Config {
	cfg := Config{
		Enabled:          strings.EqualFold(envs[EnvEnabled], "true"),
		EnrollmentTicket: envs[EnvEnrollmentTicket],
	}
	// Strip every HARNESS_PC_* value, including the retired raw-token contract.
	for key := range envs {
		if strings.HasPrefix(key, "HARNESS_PC_") {
			cfg.contractPresent = true
			if key != EnvEnabled && key != EnvEnrollmentTicket {
				cfg.unexpectedField = true
			}
			delete(envs, key)
		}
	}
	return cfg
}

// ValidateEnrollmentRequest rejects incomplete initial requests before network
// or filesystem mutation.
func ValidateEnrollmentRequest(cfg *Config) error {
	if cfg == nil {
		return fmt.Errorf("pc: enrollment configuration is required")
	}
	if !cfg.Enabled {
		if cfg.contractPresent {
			return fmt.Errorf("pc: enrollment fields require HARNESS_PC_ENABLED=true")
		}
		return nil
	}
	if cfg.unexpectedField {
		return fmt.Errorf("pc: unsupported HARNESS_PC_* enrollment field")
	}
	if strings.TrimSpace(cfg.EnrollmentTicket) == "" {
		return fmt.Errorf("pc: enrollment ticket is required when enabled")
	}
	return nil
}

// ValidateExchange validates only the server-authoritative exchange response.
// JWT signature verification remains Tailscale WIF's responsibility.
func ValidateExchange(cfg *Config, now time.Time) error { //nolint:gocyclo // Fail-closed validation intentionally checks every field.
	if cfg == nil {
		return fmt.Errorf("pc: enrollment exchange response is incomplete")
	}
	if cfg.ClientID == "" || cfg.OIDCToken == "" || cfg.Hostname == "" {
		return fmt.Errorf("pc: enrollment exchange response is incomplete")
	}
	if cfg.Tag != DefaultTag {
		return fmt.Errorf("pc: enrollment exchange returned an unsupported tag")
	}
	if !validHostname(cfg.Hostname) {
		return fmt.Errorf("pc: enrollment exchange returned an invalid hostname")
	}
	if cfg.BindingGeneration == 0 {
		return fmt.Errorf("pc: invalid binding generation")
	}
	if !cfg.ExpiresAt.After(now) || cfg.ExpiresAt.After(now.Add(maxOIDCTokenLifetime+oidcClockSkew)) {
		return fmt.Errorf("pc: enrollment exchange returned an invalid expiry")
	}
	segments := strings.Split(cfg.OIDCToken, ".")
	if len(segments) != jwtSegmentCount {
		return fmt.Errorf("pc: OIDC token must be a JWT")
	}
	payload, err := base64.RawURLEncoding.DecodeString(segments[1])
	if err != nil {
		return fmt.Errorf("pc: OIDC token payload is invalid")
	}
	var claims struct {
		IssuedAt  json.Number     `json:"iat"`
		ExpiresAt json.Number     `json:"exp"`
		Audience  json.RawMessage `json:"aud"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return fmt.Errorf("pc: OIDC token payload is invalid")
	}
	issuedAt, issuedErr := claims.IssuedAt.Int64()
	expiresAt, expiresErr := claims.ExpiresAt.Int64()
	if issuedErr != nil || expiresErr != nil || issuedAt <= 0 || expiresAt <= issuedAt {
		return fmt.Errorf("pc: OIDC token must contain valid iat and exp claims")
	}
	if len(claims.Audience) == 0 || string(claims.Audience) == "null" {
		return fmt.Errorf("pc: OIDC token must contain an audience claim")
	}
	if time.Duration(expiresAt-issuedAt)*time.Second > maxOIDCTokenLifetime {
		return fmt.Errorf("pc: OIDC token lifetime exceeds five minutes")
	}
	nowUnix := now.Unix()
	if expiresAt <= nowUnix || issuedAt > nowUnix+int64(oidcClockSkew.Seconds()) {
		return fmt.Errorf("pc: OIDC token is expired or not yet valid")
	}
	if delta := cfg.ExpiresAt.Unix() - expiresAt; delta < -1 || delta > 1 {
		return fmt.Errorf("pc: enrollment expiry does not match the OIDC token")
	}
	return nil
}

func validHostname(value string) bool {
	if value == "" || len(value) > 63 || value[0] == '-' || value[len(value)-1] == '-' {
		return false
	}
	for _, character := range value {
		if (character >= 'a' && character <= 'z') ||
			(character >= 'A' && character <= 'Z') ||
			(character >= '0' && character <= '9') ||
			character == '-' {
			continue
		}
		return false
	}
	return true
}
