// Copyright 2026 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package handler

import (
	"testing"

	"github.com/harness/lite-engine/pc"
	"github.com/stretchr/testify/require"
)

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
