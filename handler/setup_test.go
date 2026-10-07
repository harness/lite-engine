// Copyright 2026 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package handler

import (
	"os"
	"testing"

	"github.com/harness/lite-engine/pc"
	"github.com/stretchr/testify/require"
)

func TestSetHarnessEnvs_IncludesTmpPathForGoSavings(t *testing.T) {
	keys := []string{
		"HARNESS_EXECUTION_ID",
		"HARNESS_DELEGATE_TASK_ID",
		"HARNESS_TMP_PATH",
		"HARNESS_STAGE_ID",
		"HARNESS_BUILD_ID",
	}
	prev := make(map[string]string, len(keys))
	for _, key := range keys {
		prev[key] = os.Getenv(key)
		t.Setenv(key, "")
	}
	t.Cleanup(func() {
		for key, value := range prev {
			if value == "" {
				_ = os.Unsetenv(key)
			} else {
				_ = os.Setenv(key, value)
			}
		}
	})

	setHarnessEnvs(map[string]string{
		"HARNESS_EXECUTION_ID":     "exec-1",
		"HARNESS_DELEGATE_TASK_ID": "task-1",
		"HARNESS_TMP_PATH":         "/tmp/",
		"HARNESS_STAGE_ID":         "stage-1",
		"HARNESS_BUILD_ID":         "42",
		"UNRELATED":                "ignore-me",
	})

	require.Equal(t, "exec-1", os.Getenv("HARNESS_EXECUTION_ID"))
	require.Equal(t, "task-1", os.Getenv("HARNESS_DELEGATE_TASK_ID"))
	require.Equal(t, "/tmp/", os.Getenv("HARNESS_TMP_PATH"))
	require.Equal(t, "stage-1", os.Getenv("HARNESS_STAGE_ID"))
	require.Equal(t, "42", os.Getenv("HARNESS_BUILD_ID"))
	require.Empty(t, os.Getenv("UNRELATED"))
}

func TestPrivateConnectivitySetupGuardRecognizesOnlyTheCompletedSetup(t *testing.T) {
	guard := &privateConnectivitySetupGuard{}
	cfg := pc.Config{Enabled: true, ClientID: "client", Hostname: "stage-1", Tag: "tag:ci-runner"}

	replayed, err := guard.isCompletedReplay(cfg, false)
	require.NoError(t, err)
	require.False(t, replayed)

	guard.markCompleted(cfg)
	replayed, err = guard.isCompletedReplay(cfg, true)
	require.NoError(t, err)
	require.True(t, replayed)

	different := cfg
	different.Hostname = "stage-2"
	replayed, err = guard.isCompletedReplay(different, true)
	require.ErrorContains(t, err, "different setup")
	require.False(t, replayed)
}

func TestPrivateConnectivitySetupGuardResetsAfterCleanup(t *testing.T) {
	guard := &privateConnectivitySetupGuard{}
	cfg := pc.Config{Enabled: true, ClientID: "client", Hostname: "stage-1", Tag: "tag:ci-runner"}
	guard.markCompleted(cfg)

	replayed, err := guard.isCompletedReplay(cfg, false)
	require.NoError(t, err)
	require.False(t, replayed)

	_, err = guard.isCompletedReplay(cfg, true)
	require.ErrorContains(t, err, "without a completed setup")
}
