// Copyright 2022 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package server

import (
	"bytes"
	"context"
	"os"
	"os/signal"

	"github.com/harness/lite-engine/config"
	"github.com/harness/lite-engine/engine"
	"github.com/harness/lite-engine/engine/docker"
	"github.com/harness/lite-engine/handler"
	"github.com/harness/lite-engine/internal/safego"
	"github.com/harness/lite-engine/logger"
	"github.com/harness/lite-engine/pipeline/runtime"
	"github.com/harness/lite-engine/server"
	"github.com/harness/lite-engine/setup"

	"github.com/alecthomas/kingpin/v2"
	"github.com/harness/godotenv/v3"
	"github.com/sirupsen/logrus"
)

type serverCommand struct {
	envfile string
}

func (c *serverCommand) run(*kingpin.ParseContext) error {
	if c.envfile != "" {
		loadEnvErr := godotenv.Overload(c.envfile)
		if loadEnvErr != nil {
			logrus.
				WithError(loadEnvErr).
				Errorln("cannot load env file")
		}
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

	stepExecutor := runtime.NewStepExecutor(engine)

	// create the http serverInstance.
	serverInstance := server.Server{
		Addr:     loadedConfig.Server.Bind,
		Handler:  handler.Handler(&loadedConfig, engine, stepExecutor),
		CAFile:   loadedConfig.Server.CACertFile, // CA certificate file
		CertFile: loadedConfig.Server.CertFile,   // Server certificate PEM file
		KeyFile:  loadedConfig.Server.KeyFile,    // Server key file
		Insecure: loadedConfig.Server.Insecure,   // Skip server certificate verification
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
	safego.SafeGo("signal_handler", func() {
		select {
		case val := <-s:
			logrus.Infof("received OS Signal to exit server: %s", val)
			cancel()
		case <-ctx.Done():
			logrus.Infoln("received a done signal to exit server")
		}
	})

	logrus.Infof("server listening at port %s", loadedConfig.Server.Bind)
	// run the setup checks / installation
	if loadedConfig.Server.SkipPrepareServer {
		logrus.Infoln("skipping prepare server eg install docker / git")
	} else {
		setup.PrepareSystem()
	}
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
