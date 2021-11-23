package cli

import (
	"os"

	"github.com/harness/lite-engine/cli/certs"
	"github.com/harness/lite-engine/cli/client"
	"github.com/harness/lite-engine/cli/server"
	"github.com/harness/lite-engine/cli/service"

	"gopkg.in/alecthomas/kingpin.v2"
)

// program version
var version = "0.0.1"

// Command parses the command line arguments and then executes a
// subcommand program.
func Command() {
	app := kingpin.New("lite-engine", "Lite engine to execute steps")

	server.Register(app)
	certs.Register(app)
	client.Register(app)
	service.Register(app)

	kingpin.Version(version)
	kingpin.MustParse(app.Parse(os.Args[1:]))
}
