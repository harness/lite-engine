// Copyright 2022 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package server

import (
	"bytes"
	"context"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	goruntime "runtime"

	"github.com/harness/lite-engine/config"
	"github.com/harness/lite-engine/engine"
	"github.com/harness/lite-engine/engine/docker"
	"github.com/harness/lite-engine/engine/spec"
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

	// Workload Identity: serve the mint endpoint so the in-step hcli can obtain OIDC tokens. The main
	// server below enforces mTLS (client cert), which the in-step hcli cannot present, and the VM
	// firewall only opens 9079 - so the mint endpoint is served on a Unix socket that lite-engine
	// bind-mounts into each step container (no port, no mTLS, no DNS). Authorized by the opaque per-step
	// handle; only short-lived OIDC tokens cross it.
	startWorkloadIdentityMintServer()

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
	stopGoroutineDumpSignals := startGoroutineDumpSignalHandler(ctx, "")
	s := make(chan os.Signal, 1)
	signal.Notify(s, os.Interrupt)
	defer func() {
		stopGoroutineDumpSignals()
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

// startWorkloadIdentityMintServer serves the WI mint endpoint. On Linux/Mac it listens on a Unix socket
// in a host dir that lite-engine bind-mounts into each step container (see engine/docker/convert.go), so
// the in-step hcli reaches it without a network port. On Windows it falls back to a plain-HTTP TCP
// listener (named-pipe support is a follow-up; the VM firewall would also need the port opened).
func startWorkloadIdentityMintServer() {
	if goruntime.GOOS == "windows" {
		bind := os.Getenv("HARNESS_WI_MINT_BIND")
		if bind == "" {
			bind = ":9080"
		}
		safego.SafeGo("wi_mint_server", func() {
			logrus.Infof("workload-identity mint server (tcp) listening at %s", bind)
			if err := http.ListenAndServe(bind, handler.MintHandler()); err != nil {
				logrus.WithError(err).Errorln("workload-identity mint server stopped")
			}
		})
		return
	}

	socketPath := filepath.Join(spec.WISocketHostDir, spec.WISocketName)
	if err := os.MkdirAll(spec.WISocketHostDir, 0o755); err != nil {
		logrus.WithError(err).Errorln("workload-identity: failed to create mint socket dir")
		return
	}
	// Remove any stale socket from a previous lite-engine run before binding.
	if err := os.Remove(socketPath); err != nil && !os.IsNotExist(err) {
		logrus.WithError(err).Warnln("workload-identity: failed to remove stale mint socket")
	}
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		logrus.WithError(err).Errorln("workload-identity: failed to listen on mint socket")
		return
	}
	// World-accessible so a step container (any uid) can connect via the bind mount; the opaque per-step
	// handle is the capability that authorizes minting.
	if err := os.Chmod(socketPath, 0o666); err != nil { //nolint:gosec
		logrus.WithError(err).Warnln("workload-identity: failed to chmod mint socket")
	}
	safego.SafeGo("wi_mint_server", func() {
		logrus.Infof("workload-identity mint server (unix) listening at %s", socketPath)
		if err := http.Serve(listener, handler.MintHandler()); err != nil {
			logrus.WithError(err).Errorln("workload-identity mint server stopped")
		}
	})
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
