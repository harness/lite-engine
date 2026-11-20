// Copyright 2026 Harness Inc. All rights reserved.
// Use of this source code is governed by the PolyForm Free Trial 1.0.0 license
// that can be found in the licenses directory at the root of this repository, also available at
// https://polyformproject.org/wp-content/uploads/2020/05/PolyForm-Free-Trial-1.0.0.txt.

// Package sidecarcmd is the lite-engine subcommand that runs the egress sidecar.
package sidecarcmd

import (
	"github.com/alecthomas/kingpin/v2"

	"github.com/harness/lite-engine/egress/sidecar"
)

// Register adds the egress-sidecar command. The redirect starts it; an operator
// does not.
func Register(app *kingpin.Application) {
	app.Command(sidecar.ProcessName, "run the hosted egress sidecar").
		Action(func(*kingpin.ParseContext) error { return sidecar.Run() })
}
