// Copyright 2026 Harness Inc. All rights reserved.
// Use of this source code is governed by the PolyForm Free Trial 1.0.0 license
// that can be found in the licenses directory at the root of this repository, also available at
// https://polyformproject.org/wp-content/uploads/2020/05/PolyForm-Free-Trial-1.0.0.txt.

// Command egress-sidecar is the process the hosted Linux image carries.
// Lite-engine does not start it. CI-26163 does, after it writes the credential
// and installs the redirect.
package main

import (
	"fmt"
	"os"

	"github.com/harness/lite-engine/egress/sidecar"
)

func main() {
	cfg, err := sidecar.ConfigFromEnv()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	srv, err := sidecar.New(&cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := srv.ListenAndServe(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
