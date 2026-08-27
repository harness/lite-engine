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

	runnerTag   = "tag:ci-runner"
	joinTimeout = 20 * time.Second
)

// Config holds the private connectivity configuration extracted from HARNESS_PC_* env vars.
// All fields are extracted and the source envs are stripped before any further processing.
type Config struct {
	Enabled bool

	ClientID  string
	OIDCToken string
	Hostname  string
	Tag       string
}

// ExtractAndValidate removes every reserved field immediately after setup decoding and validates
// the enabled contract before any host mutation. Callers must add the returned token to masking.
func ExtractAndValidate(envs map[string]string) (Config, error) {
	enabledValue := strings.TrimSpace(envs[EnvEnabled])
	cfg := Config{
		ClientID:  envs[EnvClientID],
		OIDCToken: envs[EnvOIDCToken],
		Hostname:  envs[EnvHostname],
		Tag:       envs[EnvTag],
	}
	for key := range envs {
		if strings.HasPrefix(key, "HARNESS_PC_") {
			delete(envs, key)
		}
	}
	if enabledValue == "" || strings.EqualFold(enabledValue, "false") {
		return cfg, nil
	}
	if !strings.EqualFold(enabledValue, "true") {
		return cfg, fmt.Errorf("pc: private connectivity enabled value must be true or false")
	}
	cfg.Enabled = true
	if strings.TrimSpace(cfg.ClientID) == "" || strings.TrimSpace(cfg.OIDCToken) == "" || strings.TrimSpace(cfg.Hostname) == "" ||
		strings.TrimSpace(cfg.Tag) == "" {
		return cfg, fmt.Errorf("pc: private connectivity identity is incomplete")
	}
	// The tag is server-owned; accepting an arbitrary tag would expand the runner's ACL identity.
	if cfg.Tag != runnerTag {
		return cfg, fmt.Errorf("pc: unsupported private connectivity tag")
	}
	return cfg, nil
}
