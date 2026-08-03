// Copyright 2026 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

// Package pc implements Harness Cloud Private Connectivity lifecycle for lite-engine.
//
// Canonical /setup order:
//  1. Decode SetupRequest.
//  2. ExtractAndStrip — extract the CI-issued workload identity, mask the JWT, and strip all fields.
//  3. Reject PC combined with an egress proxy.
//  4. Repair Linux arm64 clock drift before validating the time-bound JWT.
//  5. Validate the complete contract.
//  6. JoinAndConfigure revalidates the contract and the prebaked runtime immediately before
//     joining with WIF (file-backed JWT, never argv or os.Setenv).
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
	"strconv"
	"strings"
	"time"
)

// On-VM env var names — FROZEN. Do NOT change without a coordinated update
// to ci-manager VmInitializeTaskParamsBuilder and the InternalApi contract.
const (
	EnvEnabled           = "HARNESS_PC_ENABLED"
	EnvClientID          = "HARNESS_PC_CLIENT_ID"
	EnvOIDCToken         = "HARNESS_PC_OIDC_TOKEN"
	EnvHostname          = "HARNESS_PC_HOSTNAME"
	EnvTag               = "HARNESS_PC_TAG"
	EnvBindingGeneration = "HARNESS_PC_BINDING_GENERATION"

	DefaultTag = "tag:ci-runner"
	// ContractVersion identifies the cross-repository DRA/LE lifecycle contract.
	ContractVersion = "v2"
	JoinTimeout     = 90 * time.Second

	maxOIDCTokenLifetime = 60 * time.Minute
	oidcClockSkew        = 30 * time.Second
	jwtSegmentCount      = 3
)

// Config holds the private connectivity configuration extracted from HARNESS_PC_* env vars.
// All fields are extracted and the source envs are stripped before any further processing.
type Config struct {
	Enabled bool

	ClientID          string
	OIDCToken         string
	Hostname          string
	Tag               string
	BindingGeneration uint64
	contractPresent   bool
	unexpectedField   bool
	invalidGeneration bool
}

// ExtractAndStrip reads all HARNESS_PC_* values from envs and removes every key.
//
// MUST be called immediately after decoding SetupRequest, before setProxyEnvs, setHarnessEnvs,
// state.Set, cfg construction, or any logging of envs.
//
// The returned Config.OIDCToken is secret and must be added to the secrets masking list
// before calling state.Set(s.Secrets, ...).
func ExtractAndStrip(envs map[string]string) Config {
	cfg := Config{
		Enabled:   strings.EqualFold(envs[EnvEnabled], "true"),
		ClientID:  envs[EnvClientID],
		OIDCToken: envs[EnvOIDCToken],
		Hostname:  envs[EnvHostname],
		Tag:       envs[EnvTag],
	}
	if rawGeneration := strings.TrimSpace(envs[EnvBindingGeneration]); rawGeneration != "" {
		generation, err := strconv.ParseUint(rawGeneration, 10, 64)
		cfg.BindingGeneration = generation
		cfg.invalidGeneration = err != nil
	}
	for key := range envs {
		if strings.HasPrefix(key, "HARNESS_PC_") {
			cfg.contractPresent = true
			if key != EnvEnabled && key != EnvClientID && key != EnvOIDCToken && key != EnvHostname &&
				key != EnvTag && key != EnvBindingGeneration {
				cfg.unexpectedField = true
			}
			delete(envs, key)
		}
	}
	return cfg
}

// Validate rejects incomplete or expired CI-issued identity before network or filesystem mutation.
// JWT signature verification remains Tailscale WIF's responsibility.
func Validate(cfg *Config, now time.Time) error { //nolint:gocyclo // Fail-closed validation intentionally checks every field.
	if cfg == nil {
		return fmt.Errorf("pc: private connectivity configuration is required")
	}
	if !cfg.Enabled {
		if cfg.contractPresent {
			return fmt.Errorf("pc: private connectivity fields require HARNESS_PC_ENABLED=true")
		}
		return nil
	}
	if cfg.unexpectedField {
		return fmt.Errorf("pc: unsupported HARNESS_PC_* field")
	}
	if cfg.ClientID == "" || cfg.OIDCToken == "" || cfg.Hostname == "" {
		return fmt.Errorf("pc: private connectivity identity is incomplete")
	}
	if cfg.Tag != DefaultTag {
		return fmt.Errorf("pc: unsupported private connectivity tag")
	}
	if !validHostname(cfg.Hostname) {
		return fmt.Errorf("pc: invalid private connectivity hostname")
	}
	if cfg.invalidGeneration || cfg.BindingGeneration == 0 {
		return fmt.Errorf("pc: invalid binding generation")
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
	if expiresAt-issuedAt > int64(maxOIDCTokenLifetime/time.Second) {
		return fmt.Errorf("pc: OIDC token lifetime exceeds configured max (%s)",
			maxOIDCTokenLifetime)
	}
	nowUnix := now.Unix()
	if expiresAt <= nowUnix || issuedAt > nowUnix+int64(oidcClockSkew.Seconds()) {
		return fmt.Errorf("pc: OIDC token is expired or not yet valid")
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
