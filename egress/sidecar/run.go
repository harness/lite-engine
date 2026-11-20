// Copyright 2026 Harness Inc. All rights reserved.
// Use of this source code is governed by the PolyForm Free Trial 1.0.0 license
// that can be found in the licenses directory at the root of this repository, also available at
// https://polyformproject.org/wp-content/uploads/2020/05/PolyForm-Free-Trial-1.0.0.txt.

package sidecar

import "fmt"

// ProcessName is the process name and the lite-engine subcommand. The redirect
// starts this binary as "lite-engine egress-sidecar" and checks that argument
// before it will signal the process.
const ProcessName = "egress-sidecar"

// Run reads the process environment and serves. lite-engine starts it as its
// own subprocess; the command is part of the lite-engine binary.
func Run() error {
	cfg, err := ConfigFromEnv()
	if err != nil {
		return err
	}
	// The executable name is lite-engine, which is also the parent. The stop
	// check refuses to signal a process it cannot tell from that parent.
	if err := setProcessName(ProcessName); err != nil {
		return fmt.Errorf("sidecar process name: %w", err)
	}
	srv, err := New(&cfg)
	if err != nil {
		return err
	}
	return srv.ListenAndServe()
}
