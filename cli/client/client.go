package client

import (
	"context"
	"fmt"

	"github.com/harness/lite-engine/api"
	"github.com/harness/lite-engine/config"
	"github.com/harness/lite-engine/engine/spec"

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

	client, err := NewHTTPClient(
		fmt.Sprintf("https://%s/", loadedConfig.Client.Bind),
		loadedConfig.ServerName, loadedConfig.Client.CaCertFile, loadedConfig.Client.CertFile, loadedConfig.Client.KeyFile)
	if err != nil {
		logrus.WithError(err).
			Errorln("failed to create client")
		return errors.Wrap(err, "failed to create client")
	}

	if c.runStage {
		return runStage(client, c.remoteLog)
	}
	return checkServerHealth(client)
}

func checkServerHealth(client *HTTPClient) error {
	return client.Health(context.Background())
}

func runStage(client *HTTPClient, remoteLog bool) error { // nolint:funlen
	ctx := context.Background()
	defer func() {
		logrus.Infof("Starting destroy")
		if _, err := client.Destroy(ctx, &api.DestroyRequest{}); err != nil {
			logrus.WithError(err).Errorln("destroy call failed")
			panic(err)
		}
	}()

	workDir := "/drone/src"

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
	}
	if remoteLog {
		setupParams.LogConfig = api.LogConfig{
			URL:            "http://localhost:8079",
			AccountID:      "kmpy",
			Token:          "token",
			IndirectUpload: true,
		}
	}
	logrus.Infof("Starting setup")
	if _, err := client.Setup(ctx, setupParams); err != nil {
		logrus.WithError(err).Errorln("setup call failed")
		return err
	}
	logrus.Infof("completed setup")

	// Execute step1
	sid1 := "step1"
	s1 := &api.StartStepRequest{
		ID:    sid1,
		Kind:  api.Run,
		Image: "alpine:3.12",
		Volumes: []*spec.VolumeMount{
			{
				Name: "_workspace",
				Path: workDir,
			},
		},
		WorkingDir: workDir,
		LogKey:     sid1,
	}
	s1.Run.Command = []string{"set -xe; pwd; echo drone; echo hello world > foo; cat foo"}
	s1.Run.Entrypoint = []string{"sh", "-c"}

	if err := executeStep(ctx, s1, client); err != nil {
		return err
	}

	// Execute Step2
	sid2 := "step2"
	s2 := &api.StartStepRequest{
		ID:    sid2,
		Kind:  api.Run,
		Image: "alpine:3.12",
		Volumes: []*spec.VolumeMount{
			{
				Name: "_workspace",
				Path: workDir,
			},
		},
		WorkingDir: workDir,
		OutputVars: []string{"hello"},
		LogKey:     sid2,
	}
	s2.Run.Command = []string{"set -xe; pwd; cat foo; export hello=world"}
	s2.Run.Entrypoint = []string{"sh", "-c"}

	if err := executeStep(ctx, s2, client); err != nil {
		return err
	}

	return nil
}

func executeStep(ctx context.Context, step *api.StartStepRequest, client *HTTPClient) error {
	logrus.Infof("Starting step %s", step.ID)
	if _, err := client.StartStep(ctx, step); err != nil {
		logrus.WithError(err).Errorf("start %s call failed", step.ID)
		return err
	}
	logrus.Infof("Polling %s", step.ID)
	res, err := client.PollStep(ctx, &api.PollStepRequest{ID: step.ID})
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
