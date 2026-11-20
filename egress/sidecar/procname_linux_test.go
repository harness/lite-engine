// Copyright 2026 Harness Inc. All rights reserved.
// Use of this source code is governed by the PolyForm Free Trial 1.0.0 license
// that can be found in the licenses directory at the root of this repository, also available at
// https://polyformproject.org/wp-content/uploads/2020/05/PolyForm-Free-Trial-1.0.0.txt.

//go:build linux

package sidecar

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestMain(m *testing.M) {
	if os.Getenv("PROCNAME") == "1" {
		// TestMain runs on the main thread, which is the name ps reports.
		if err := setProcessName(ProcessName); err != nil {
			os.Exit(2)
		}
		got, err := os.ReadFile("/proc/self/comm")
		if err != nil || strings.TrimSpace(string(got)) != ProcessName {
			os.Exit(3)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestSetProcessName(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=^$")
	cmd.Env = append(os.Environ(), "PROCNAME=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("setProcessName: %v\n%s", err, out)
	}
}
