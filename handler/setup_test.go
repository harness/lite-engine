// Copyright 2026 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package handler

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidatePrivateConnectivityReuse(t *testing.T) {
	require.NoError(t, validatePrivateConnectivityReuse(false))
	require.ErrorContains(t, validatePrivateConnectivityReuse(true), "discard this VM")
}
