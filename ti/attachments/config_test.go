// Copyright 2026 Harness Inc. All rights reserved.
// Use of this source code is governed by the PolyForm Free Trial 1.0.0 license
// that can be found in the licenses directory at the root of this repository, also available at
// https://polyformproject.org/wp-content/uploads/2020/05/PolyForm-Free-Trial-1.0.0.txt.

package attachments

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCheckConfig(t *testing.T) {
	full := Config{URL: "https://har", AccountID: "acc", Registry: "reg", PipelineUniqueID: "pipe",
		BuildID: "7", Token: "secret-token", Workspace: "/ws"}

	t.Run("complete", func(t *testing.T) {
		cfg, err := checkConfig(&full)
		require.NoError(t, err)
		assert.Equal(t, full, cfg)
	})

	t.Run("values are trimmed", func(t *testing.T) {
		padded := full
		padded.URL = " https://har "
		padded.Token = "secret-token\n"
		cfg, err := checkConfig(&padded)
		require.NoError(t, err)
		assert.Equal(t, full, cfg)
	})

	t.Run("none of the CI Manager values means the feature is off", func(t *testing.T) {
		_, err := checkConfig(&Config{AccountID: "a", BuildID: "1", Workspace: "/ws"})
		require.ErrorIs(t, err, ErrNotConfigured)
		assert.NotErrorIs(t, err, ErrIncomplete)
	})

	t.Run("some values missing is incomplete", func(t *testing.T) {
		_, err := checkConfig(&Config{URL: "https://har", Token: "secret-token"})
		require.ErrorIs(t, err, ErrNotConfigured)
		require.ErrorIs(t, err, ErrIncomplete)
		assert.Contains(t, err.Error(), "account_id, build_id, pipeline_unique_id, registry, workspace not set")
		assert.NotContains(t, err.Error(), "secret")
	})

	t.Run("a blank value counts as missing", func(t *testing.T) {
		blank := full
		blank.Workspace = "  "
		_, err := checkConfig(&blank)
		require.ErrorIs(t, err, ErrIncomplete)
		assert.Contains(t, err.Error(), "workspace")
	})
}
