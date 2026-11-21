// Copyright 2026 Harness Inc. All rights reserved.
// Use of this source code is governed by the PolyForm Free Trial 1.0.0 license
// that can be found in the licenses directory at the root of this repository, also available at
// https://polyformproject.org/wp-content/uploads/2020/05/PolyForm-Free-Trial-1.0.0.txt.

//go:build linux

package redirect

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// Right after exec the kernel can show the sidecar with an empty command line.
// Readiness has to follow the pid it started, so a process that never shows the
// sidecar command stands in for that window.
func TestSidecarAliveFollowsThePidNotTheCommandLine(t *testing.T) {
	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	pid := cmd.Process.Pid
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })

	if isSidecarProcess(pid) {
		t.Fatal("sleep passed the stop check")
	}
	if !sidecarAlive(pid) {
		t.Fatal("a running process the install started is not alive")
	}
	if sidecarListening(pid) {
		t.Fatal("a process without the sidecar port counts as listening")
	}

	_ = cmd.Process.Kill()
	_ = cmd.Wait()
	if sidecarAlive(pid) {
		t.Fatal("an exited process is alive")
	}
}

func TestWaitListeningFailsAtOnceWhenTheSidecarExits(t *testing.T) {
	cmd := exec.Command("true")
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	err := waitListening(context.Background(), cmd.Process.Pid)
	if err == nil || !strings.Contains(err.Error(), "exited") {
		t.Fatalf("waitListening = %v, want the sidecar exited error", err)
	}
	if elapsed := time.Since(start); elapsed >= readyTimeout {
		t.Fatalf("waitListening took %s, want an immediate failure", elapsed)
	}
}
