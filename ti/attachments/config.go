// Copyright 2026 Harness Inc. All rights reserved.
// Use of this source code is governed by the PolyForm Free Trial 1.0.0 license
// that can be found in the licenses directory at the root of this repository, also available at
// https://polyformproject.org/wp-content/uploads/2020/05/PolyForm-Free-Trial-1.0.0.txt.

// Package attachments uploads the result directories of tests (Playwright traces and videos) that are referenced by
// [[ATTACHMENT|path]] lines in JUnit reports to the Harness Artifact Registry (HAR).
package attachments

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/harness/lite-engine/api"
)

// errNotConfigured means CI Manager sent no registry config with the step, the normal case for a step that does
// not upload test attachments. errIncomplete means it sent only part of one.
var (
	errNotConfigured = errors.New("test attachment upload is not configured for this step")
	errIncomplete    = errors.New("configuration is incomplete")
)

// checkConfig returns the registry config of a step, which CI Manager sends in the step request only for steps that
// upload test attachments (it must not be read from the step or stage environment, which the user controls), with
// surrounding spaces trimmed, or an error if it cannot be used.
func checkConfig(in *api.HarConfig) (api.HarConfig, error) {
	c := api.HarConfig{
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
		return api.HarConfig{}, fmt.Errorf("%w: %s not set", errNotConfigured, strings.Join(missing, ", "))
	}
	return api.HarConfig{}, fmt.Errorf("%w: %s not set", errIncomplete, strings.Join(missing, ", "))
}
