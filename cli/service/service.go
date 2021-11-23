package service

import (
	"bytes"
	"os"

	"github.com/harness/lite-engine/config"
	"github.com/harness/lite-engine/logger"
	"github.com/harness/lite-engine/setup"

	"github.com/joho/godotenv"
	"github.com/sirupsen/logrus"
	"gopkg.in/alecthomas/kingpin.v2"
)

type serviceCommand struct {
	envfile string
}

func (c *serviceCommand) run(*kingpin.ParseContext) error {
	loadEnvErr := godotenv.Load(c.envfile)
	if loadEnvErr != nil {
		logrus.
			WithError(loadEnvErr).
			Errorln("cannot load env file")
	}
	// load the system configuration from the environment.
	loadedConfig, err := config.Load()
	if err != nil {
		logrus.WithError(err).
			Errorln("cannot load the service configuration")
		return err
	}

	// init the system logging.
	initLogging(&loadedConfig)

	// install windows service handler wssm, add the service and start lite-engine.
	startServiceErr := setup.PrepareService()
	return startServiceErr
}

// Register the service command.
func Register(app *kingpin.Application) {
	c := new(serviceCommand)

	cmd := app.Command("service", "start the service").
		Action(c.run)

	cmd.Flag("env-file", "environment file").
		Default(".env").
		StringVar(&c.envfile)
}

// Get stackdriver to display logs correctly https://github.com/sirupsen/logrus/issues/403
type OutputSplitter struct{}

func (splitter *OutputSplitter) Write(p []byte) (n int, err error) {
	if bytes.Contains(p, []byte("level=error")) {
		return os.Stderr.Write(p)
	}
	return os.Stdout.Write(p)
}

// helper function configures the global logger from the loaded configuration.
func initLogging(c *config.Config) {
	logrus.SetOutput(&OutputSplitter{})
	l := logrus.StandardLogger()
	logger.L = logrus.NewEntry(l)
	if c.Debug {
		l.SetLevel(logrus.DebugLevel)
	}
	if c.Trace {
		l.SetLevel(logrus.TraceLevel)
	}
}
