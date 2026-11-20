// Copyright 2026 Harness Inc. All rights reserved.
// Use of this source code is governed by the PolyForm Free Trial 1.0.0 license
// that can be found in the licenses directory at the root of this repository, also available at
// https://polyformproject.org/wp-content/uploads/2020/05/PolyForm-Free-Trial-1.0.0.txt.

package sidecar

import "testing"

func TestRunRequiresConfiguration(t *testing.T) {
	t.Setenv(envProxyAuth, "")
	t.Setenv(envProxyAuthFile, "")
	t.Setenv(envProxyHost, "")
	if err := Run(); err == nil {
		t.Fatal("Run succeeded without a proxy host or credential")
	}
}
