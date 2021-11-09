package server

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/signal"

	"github.com/harness/lite-engine/config"
	"github.com/harness/lite-engine/engine"
	"github.com/harness/lite-engine/engine/docker"
	"github.com/harness/lite-engine/executor"
	"github.com/harness/lite-engine/handler"
	"github.com/harness/lite-engine/logger"
	"github.com/harness/lite-engine/server"

	"github.com/joho/godotenv"
	"github.com/sirupsen/logrus"
	"gopkg.in/alecthomas/kingpin.v2"
)

type serverCommand struct {
	envfile string
}

func (c *serverCommand) run(*kingpin.ParseContext) error {
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

	engine, err := engine.NewEnv(docker.Opts{})
	if err != nil {
		logrus.WithError(err).
			Errorln("failed to initialize engine")
		return err
	}

	stepExecutor := executor.NewStepExecutor(engine)

	// create the http serverInstance.
	serverInstance := server.Server{
		Addr:     loadedConfig.Server.Bind,
		Handler:  handler.Handler(loadedConfig, engine, stepExecutor),
		CAFile:   loadedConfig.Server.CACertFile, // CA certificate file
		CertFile: loadedConfig.Server.CertFile,   // Server certificate PEM file
		KeyFile:  loadedConfig.Server.KeyFile,    // Server key file
	}

	// trap the os signal to gracefully shutdown the http server.
	ctx := context.Background()
	ctx, cancel := context.WithCancel(ctx)
	s := make(chan os.Signal, 1)
	signal.Notify(s, os.Interrupt)
	defer func() {
		signal.Stop(s)
		cancel()
	}()
	go func() {
		select {
		case val := <-s:
			logrus.Infof("received OS Signal to exit server: %s", val)
			cancel()
		case <-ctx.Done():
			logrus.Infoln("received a done signal to exit server")
		}
	}()

	logrus.Infof(fmt.Sprintf("server listening at port %s", loadedConfig.Server.Bind))

	// starts the http server.
	err = serverInstance.Start(ctx)
	if err == context.Canceled {
		logrus.Infoln("program gracefully terminated")
		return nil
	}

	if err != nil {
		logrus.Errorf("program terminated with error: %s", err)
	}

	return err
}

// Register the server commands.
func Register(app *kingpin.Application) {
	c := new(serverCommand)

	cmd := app.Command("server", "start the server").
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
