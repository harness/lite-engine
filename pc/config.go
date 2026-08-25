// Copyright 2026 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

// Package pc implements the Lite Engine side of Cloud Private Connectivity.
package pc

import (
	"fmt"
	"strings"
	"time"
)

// On-VM env var names — FROZEN. Do NOT change without a coordinated update
// to ci-manager VmInitializeTaskParamsBuilder and the InternalApi contract.
const (
	EnvEnabled   = "HARNESS_PC_ENABLED"
	EnvClientID  = "HARNESS_PC_CLIENT_ID"
	EnvOIDCToken = "HARNESS_PC_OIDC_TOKEN" //nolint:gosec // Environment variable name, not a credential.
	EnvHostname  = "HARNESS_PC_HOSTNAME"
	EnvTag       = "HARNESS_PC_TAG"

	DefaultTag  = "tag:ci-runner"
	JoinTimeout = 20 * time.Second
)

// Config holds the private connectivity configuration extracted from HARNESS_PC_* env vars.
// All fields are extracted and the source envs are stripped before any further processing.
type Config struct {
	Enabled bool

	ClientID        string
	OIDCToken       string
	Hostname        string
	Tag             string
	contractPresent bool
	explicitlyOff   bool
	payloadPresent  bool
	unexpectedField bool
}

// ExtractAndStrip removes every reserved field immediately after setup decoding. Callers must add
// the returned OIDC token to masking before recording setup state.
func ExtractAndStrip(envs map[string]string) Config {
	enabledValue, enabledPresent := envs[EnvEnabled]
	cfg := Config{
		Enabled:   strings.EqualFold(strings.TrimSpace(enabledValue), "true"),
		ClientID:  envs[EnvClientID],
		OIDCToken: envs[EnvOIDCToken],
		Hostname:  envs[EnvHostname],
		Tag:       envs[EnvTag],
		explicitlyOff: enabledPresent &&
			strings.EqualFold(strings.TrimSpace(enabledValue), "false"),
	}
	for key := range envs {
		if strings.HasPrefix(key, "HARNESS_PC_") {
			cfg.contractPresent = true
			if key != EnvEnabled {
				cfg.payloadPresent = true
			}
			if key != EnvEnabled && key != EnvClientID && key != EnvOIDCToken && key != EnvHostname && key != EnvTag {
				cfg.unexpectedField = true
			}
			delete(envs, key)
		}
	}
	return cfg
}

// Validate rejects an incomplete or unsupported contract before network or filesystem mutation.
// The OIDC token is intentionally opaque here; Tailscale WIF owns its claim and signature validation.
func Validate(cfg *Config) error {
	if cfg == nil {
		return fmt.Errorf("pc: private connectivity configuration is required")
	}
	if !cfg.Enabled {
		if !cfg.contractPresent || (cfg.explicitlyOff && !cfg.payloadPresent) {
			return nil
		}
		return fmt.Errorf("pc: private connectivity fields require HARNESS_PC_ENABLED=true")
	}
	if cfg.unexpectedField {
		return fmt.Errorf("pc: unsupported HARNESS_PC_* field")
	}
	if strings.TrimSpace(cfg.ClientID) == "" || cfg.OIDCToken == "" || cfg.Hostname == "" {
		return fmt.Errorf("pc: private connectivity identity is incomplete")
	}
	if cfg.Tag != DefaultTag {
		return fmt.Errorf("pc: unsupported private connectivity tag")
	}
	return nil
}
