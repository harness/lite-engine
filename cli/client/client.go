// Copyright 2022 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package client

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/harness/lite-engine/api"
	"github.com/harness/lite-engine/cli/certs"
	"github.com/harness/lite-engine/config"
	"github.com/harness/lite-engine/engine/spec"
	"github.com/harness/lite-engine/logger"

	"github.com/joho/godotenv"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"gopkg.in/alecthomas/kingpin.v2"
)

type clientCommand struct {
	envfile   string
	runStage  bool
	remoteLog bool
}

func (c *clientCommand) run(*kingpin.ParseContext) error {
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
	// setup logging
	l := logrus.StandardLogger()
	logger.L = logrus.NewEntry(l)
	if loadedConfig.Debug {
		l.SetLevel(logrus.DebugLevel)
	}
	if loadedConfig.Trace {
		l.SetLevel(logrus.TraceLevel)
	}

	var client *HTTPClient
	if loadedConfig.Client.Insecure {
		client = &HTTPClient{
			Client:   &http.Client{},
			Endpoint: fmt.Sprintf("http://%s/", loadedConfig.Client.Bind),
		}
	} else {
		// read the certificates
		ce, err := certs.ReadCerts(loadedConfig.Client.CaCertFile, loadedConfig.Client.CertFile, loadedConfig.Client.KeyFile)
		if err != nil {
			return err
		}

		client, err = NewHTTPClient(
			fmt.Sprintf("https://%s/", loadedConfig.Client.Bind),
			loadedConfig.ServerName, ce.CaCertFile, ce.CertFile, ce.KeyFile)
		if err != nil {
			logrus.WithError(err).
				Errorln("failed to create client")
			return errors.Wrap(err, "failed to create client")
		}
	}

	if c.runStage {
		return runStage(client, c.remoteLog)
	}
	return checkServerHealth(client)
}

func checkServerHealth(client *HTTPClient) error {
	response, healthErr := client.Health(context.Background())
	if healthErr != nil {
		logrus.WithError(healthErr).
			Errorln("cannot check the health of the server")
		return errors.Wrap(healthErr, "cannot check the health of the server")
	}
	logrus.WithField("response", response).Info("health check")
	return nil
}

func runStage(client *HTTPClient, remoteLog bool) error {
	ctx := context.Background()
	defer func() {
		logrus.Infof("starting destroy")
		if _, err := client.Destroy(ctx, &api.DestroyRequest{}); err != nil {
			logrus.WithError(err).Errorln("destroy call failed")
			panic(err)
		}
	}()

	const healthTimeout = time.Minute * 20

	logrus.Infof("check health")
	if _, err := client.RetryHealth(ctx, healthTimeout); err != nil {
		logrus.WithError(err).Errorln("not healthy")
		return err
	}
	logrus.Infof("healthy")

	setupParams := &api.SetupRequest{
		Volumes: []*spec.Volume{
			{
				HostPath: &spec.VolumeHostPath{
					ID:   "drone",
					Name: "_workspace",
					Path: "/tmp/lite-engine",
				},
			},
		},
		Network: spec.Network{
			ID: "drone",
		},
		Files: []*spec.File{
			{
				Path:  "/tmp/globalfolder",
				IsDir: true,
				Mode:  0777,
			},
		},
	}
	if remoteLog {
		setupParams.LogConfig = api.LogConfig{
			URL:            "http://localhost:8079",
			AccountID:      "kmpy",
			Token:          "token",
			IndirectUpload: true,
		}
	}
	logrus.Infof("starting setup")
	if _, err := client.Setup(ctx, setupParams); err != nil {
		logrus.WithError(err).Errorln("setup call failed")
		return err
	}
	logrus.Infof("completed setup")

	// run steps
	workDir := "/drone/src"
	s1 := getRunStep("step1", "set -xe; pwd; sleep 2; echo drone; echo hello world > foo; cat foo", workDir)
	if err := executeStep(ctx, s1, client); err != nil {
		return err
	}
	// execute step2
	s2 := getRunStep("step2", "set -xe; pwd; cat foo; sleep 5; export hello=world", workDir)
	s2.OutputVars = append(s2.OutputVars, "hello")
	if err := executeStep(ctx, s2, client); err != nil {
		return err
	}
	return nil
}

func getRunStep(id, cmd, workdir string) *api.StartStepRequest {
	s := &api.StartStepRequest{
		ID:    id,
		Name:  id,
		Kind:  api.Run,
		Image: "alpine:3.12",
		Volumes: []*spec.VolumeMount{
			{
				Name: "_workspace",
				Path: workdir,
			},
		},
		WorkingDir: workdir,
		LogKey:     id,
		Files: []*spec.File{
			{
				Path:  fmt.Sprintf("/tmp/globalfolder/%s", id),
				IsDir: false,
				Mode:  0777,
				Data:  cmd,
			},
		},
	}
	s.Run.Command = []string{cmd}
	s.Run.Entrypoint = []string{"sh", "-c"}
	return s
}

func executeStep(ctx context.Context, step *api.StartStepRequest, client *HTTPClient) error {
	logrus.Infof("calling starting step %s", step.ID)
	if _, err := client.StartStep(ctx, step); err != nil {
		logrus.WithError(err).Errorf("start %s call failed", step.ID)
		return err
	}
	logrus.Infof("polling %s", step.ID)

	const pollStepTimeout = time.Hour * 4

	res, err := client.RetryPollStep(ctx, &api.PollStepRequest{ID: step.ID}, pollStepTimeout)
	if err != nil {
		logrus.WithError(err).Errorf("poll %s call failed", step.ID)
		return err
	}
	logrus.WithField("response", res).Infof("%s poll completed successfully", step.ID)
	return nil
}

// Register the server commands.
func Register(app *kingpin.Application) {
	c := new(clientCommand)

	cmd := app.Command("client", "check health of the server").
		Action(c.run)

	cmd.Flag("env-file", "environment file").
		Default(".env").
		StringVar(&c.envfile)
	cmd.Flag("stage", "Run a stage").
		BoolVar(&c.runStage)
	cmd.Flag("remotelog", "Enable remote logging if client runs in stage mode").
		BoolVar(&c.remoteLog)
}
