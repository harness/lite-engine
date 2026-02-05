// Copyright 2022 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package cli

import (
	"os"

	"github.com/harness/lite-engine/cli/certs"
	"github.com/harness/lite-engine/cli/client"
	"github.com/harness/lite-engine/cli/server"
	"github.com/harness/lite-engine/version"

	"github.com/alecthomas/kingpin/v2"
)

// Command parses the command line arguments and then executes a
// subcommand program.
func Command() {
	app := kingpin.New("lite-engine", "Lite engine to execute steps")
	app.HelpFlag.Short('h')
	app.Version(version.Version)
	app.VersionFlag.Short('v')
	server.Register(app)
	certs.Register(app)
	client.Register(app)

	kingpin.MustParse(app.Parse(os.Args[1:]))
}
