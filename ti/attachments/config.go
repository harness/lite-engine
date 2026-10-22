// Copyright 2026 Harness Inc. All rights reserved.
// Use of this source code is governed by the PolyForm Free Trial 1.0.0 license
// that can be found in the licenses directory at the root of this repository, also available at
// https://polyformproject.org/wp-content/uploads/2020/05/PolyForm-Free-Trial-1.0.0.txt.

// Package attachments uploads the result directories of tests (Playwright traces, videos and
// screenshots) that are referenced by [[ATTACHMENT|path]] lines in JUnit reports to the Harness
// Artifact Registry (HAR).
package attachments

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

// ErrNotConfigured means the registry is not usable for this stage: CI Manager did not configure it
// (the normal case when native artifact storage is off) or the configuration is incomplete.
// ErrIncomplete is also wrapped in the second case so that it can be logged.
var (
	ErrNotConfigured = errors.New("test attachment upload is not configured for this stage")
	ErrIncomplete    = errors.New("configuration is incomplete")
)

// Config is the registry configuration of a stage. CI Manager sends it in the setup request, and it must not
// be read from the step or stage environment, which the user controls.
type Config struct {
	URL              string
	AccountID        string
	Registry         string
	PipelineUniqueID string
	BuildID          string
	Token            string
	// Workspace is the directory attachments must be inside.
	Workspace string
}

// checkConfig returns the config with surrounding spaces trimmed, or an error if it cannot be used.
func checkConfig(in *Config) (Config, error) {
	c := Config{
		URL:              strings.TrimSpace(in.URL),
		AccountID:        strings.TrimSpace(in.AccountID),
		Registry:         strings.TrimSpace(in.Registry),
		PipelineUniqueID: strings.TrimSpace(in.PipelineUniqueID),
		BuildID:          strings.TrimSpace(in.BuildID),
		Token:            strings.TrimSpace(in.Token),
		Workspace:        strings.TrimSpace(in.Workspace),
	}
	var missing []string
	for name, v := range map[string]string{"url": c.URL, "account_id": c.AccountID, "registry": c.Registry,
		"pipeline_unique_id": c.PipelineUniqueID, "build_id": c.BuildID, "token": c.Token,
		"workspace": c.Workspace} {
		if v == "" {
			missing = append(missing, name)
		}
	}
	if len(missing) == 0 {
		return c, nil
	}
	sort.Strings(missing)
	// CI Manager sets these four together when the feature is on, so none of them means it is off.
	if c.URL == "" && c.Registry == "" && c.PipelineUniqueID == "" && c.Token == "" {
		return Config{}, fmt.Errorf("%w: %s not set", ErrNotConfigured, strings.Join(missing, ", "))
	}
	return Config{}, fmt.Errorf("%w (%w): %s not set", ErrNotConfigured, ErrIncomplete, strings.Join(missing, ", "))
}
