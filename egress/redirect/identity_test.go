// Copyright 2026 Harness Inc. All rights reserved.
// Use of this source code is governed by the PolyForm Free Trial 1.0.0 license
// that can be found in the licenses directory at the root of this repository, also available at
// https://polyformproject.org/wp-content/uploads/2020/05/PolyForm-Free-Trial-1.0.0.txt.

package redirect

import "testing"

func TestIsSidecarArgs(t *testing.T) {
	sidecarCmd := []byte("/usr/bin/lite-engine\x00egress-sidecar\x00")
	if !isSidecarArgs(sidecarCmd) {
		t.Fatal("the sidecar command was not recognized")
	}
	parent := []byte("/usr/bin/lite-engine\x00server\x00--env-file\x00/root/.env\x00")
	if isSidecarArgs(parent) {
		t.Fatal("the lite-engine server command was treated as the sidecar")
	}
	if isSidecarArgs(nil) || isSidecarArgs([]byte("/usr/bin/egress-sidecar\x00")) {
		t.Fatal("a command without the subcommand argument was accepted")
	}
}
