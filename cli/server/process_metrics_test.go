// Copyright 2026 Harness Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package server

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestProcessMetricsAddr(t *testing.T) {
	addr, ok := processMetricsAddr("", "")
	assert.False(t, ok)
	assert.Empty(t, addr)

	addr, ok = processMetricsAddr("true", "")
	assert.True(t, ok)
	assert.Equal(t, defaultProcessMetricsAddr, addr)

	_, ok = processMetricsAddr("true", "0.0.0.0:9099")
	assert.False(t, ok)

	addr, ok = processMetricsAddr("1", "localhost:9099")
	assert.True(t, ok)
	assert.Equal(t, "localhost:9099", addr)
}
