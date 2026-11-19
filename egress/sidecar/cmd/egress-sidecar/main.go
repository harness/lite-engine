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
	srv, err := sidecar.New(cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := srv.ListenAndServe(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
