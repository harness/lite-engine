// Copyright 2022 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

// Copyright 2019 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package docker

import (
	"context"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/harness/lite-engine/engine/docker/image"
	"github.com/harness/lite-engine/engine/spec"
	"github.com/harness/lite-engine/internal/docker/errors"
	"github.com/harness/lite-engine/internal/docker/jsonmessage"
	"github.com/harness/lite-engine/internal/docker/stdcopy"
	"github.com/sirupsen/logrus"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/volume"
	"github.com/docker/docker/client"
	"github.com/docker/docker/errdefs"
	"github.com/drone/runner-go/logger"
	"github.com/drone/runner-go/pipeline/runtime"
	"github.com/drone/runner-go/registry/auths"
)

const (
	imageMaxRetries           = 3
	imageRetrySleepDuration   = 50
	networkMaxRetries         = 3
	networkRetrySleepDuration = 50
	dockerImageSplitLength    = 2
	pathEnvVariable           = "PATH"
)

// Opts configures the Docker engine.
type Opts struct {
	HidePull bool
}

// Docker implements a Docker pipeline engine.
type Docker struct {
	client     client.APIClient
	hidePull   bool
	mu         sync.Mutex
	containers []string
}

// New returns a new engine.
func New(client client.APIClient, opts Opts) *Docker {
	return &Docker{
		client:     client,
		hidePull:   opts.HidePull,
		mu:         sync.Mutex{},
		containers: make([]string, 0),
	}
}

// NewEnv returns a new Engine from the environment.
func NewEnv(opts Opts) (*Docker, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv)
	if err != nil {
		return nil, err
	}
	return New(cli, opts), nil
}

// Ping pings the Docker daemon.
func (e *Docker) Ping(ctx context.Context) error {
	_, err := e.client.Ping(ctx)
	return err
}

// Setup the pipeline environment.
func (e *Docker) Setup(ctx context.Context, pipelineConfig *spec.PipelineConfig) error {
	// creates the default temporary (local) volumes
	// that are mounted into each container step.
	for _, vol := range pipelineConfig.Volumes {
		if vol.EmptyDir == nil {
			continue
		}
		_, err := e.client.VolumeCreate(ctx, volume.VolumeCreateBody{
			Name:   vol.EmptyDir.ID,
			Driver: "local",
			Labels: vol.EmptyDir.Labels,
		})
		if err != nil {
			return errors.TrimExtraInfo(err)
		}
	}

	err := e.createNetworkWithRetries(ctx, pipelineConfig)

	// launches the inernal setup steps
	// for _, step := range pipelineConfig.Internal {
	// 	if err := e.create(ctx, spec, step, ioutil.Discard); err != nil {
	// 		logger.FromContext(ctx).
	// 			WithError(err).
	// 			WithField("container", step.ID).
	// 			Errorln("cannot create tmate container")
	// 		return err
	// 	}
	// 	if err := e.start(ctx, step.ID); err != nil {
	// 		logger.FromContext(ctx).
	// 			WithError(err).
	// 			WithField("container", step.ID).
	// 			Errorln("cannot start tmate container")
	// 		return err
	// 	}
	// 	if !step.Detach {
	// 		// the internal containers perform short-lived tasks
	// 		// and should not require > 1 minute to execute.
	// 		//
	// 		// just to be on the safe side we apply a timeout to
	// 		// ensure we never block pipeline execution because we
	// 		// are waiting on an internal task.
	// 		ctx, cancel := context.WithTimeout(ctx, time.Minute)
	// 		defer cancel()
	// 		e.wait(ctx, step.ID)
	// 	}
	// }

	return errors.TrimExtraInfo(err)
}

// Destroy the pipeline environment.
func (e *Docker) Destroy(ctx context.Context, pipelineConfig *spec.PipelineConfig) error {
	removeOpts := types.ContainerRemoveOptions{
		Force:         true,
		RemoveLinks:   false,
		RemoveVolumes: true,
	}
	e.mu.Lock()
	containers := e.containers
	e.mu.Unlock()

	// stop all containers
	for _, ctrName := range containers {
		if err := e.client.ContainerKill(ctx, ctrName, "9"); err != nil {
			logrus.WithField("container", ctrName).WithField("error", err).Warnln("failed to kill container")
		}
	}

	// cleanup all containers
	for _, ctrName := range containers {
		if err := e.client.ContainerRemove(ctx, ctrName, removeOpts); err != nil {
			logrus.WithField("container", ctrName).WithField("error", err).Warnln("failed to remove container")
		}
	}

	// cleanup all volumes
	for _, vol := range pipelineConfig.Volumes {
		if vol.EmptyDir == nil {
			continue
		}
		// tempfs volumes do not have a volume entry,
		// and therefore do not require removal.
		if vol.EmptyDir.Medium == "memory" {
			continue
		}
		if err := e.client.VolumeRemove(ctx, vol.EmptyDir.ID, true); err != nil {
			logrus.WithField("volume", vol.EmptyDir.ID).WithField("error", err).Warnln("failed to remove volume")
		}
	}

	// cleanup the network
	if err := e.client.NetworkRemove(ctx, pipelineConfig.Network.ID); err != nil {
		logrus.WithField("network", pipelineConfig.Network.ID).WithField("error", err).Warnln("failed to remove network")
	}

	// notice that we never collect or return any errors.
	// this is because we silently ignore cleanup failures
	// and instead ask the system admin to periodically run
	// `docker prune` commands.
	return nil
}

// Run runs the pipeline step.
func (e *Docker) Run(ctx context.Context, pipelineConfig *spec.PipelineConfig, step *spec.Step,
	output io.Writer) (*runtime.State, error) {
	// create the container
	err := e.create(ctx, pipelineConfig, step, output)
	if err != nil {
		return nil, errors.TrimExtraInfo(err)
	}
	// start the container
	err = e.start(ctx, step.ID)
	if err != nil {
		return nil, errors.TrimExtraInfo(err)
	}
	// grab the logs from the container execution
	err = e.logs(ctx, step.ID, output)
	if err != nil {
		return nil, errors.TrimExtraInfo(err)
	}
	// wait for the response
	return e.waitRetry(ctx, step.ID)
}

//
// emulate docker commands
//

func (e *Docker) create(ctx context.Context, pipelineConfig *spec.PipelineConfig, step *spec.Step, output io.Writer) error {
	// create pull options with encoded authorization credentials.
	pullopts := types.ImagePullOptions{}
	if step.Auth != nil {
		pullopts.RegistryAuth = auths.Header(
			step.Auth.Username,
			step.Auth.Password,
		)
	}

	pulled, pullErr := e.pullDockerImage(ctx, step, pullopts, output)
	if pullErr != nil {
		return pullErr
	}

	e.dockerEnvOverride(ctx, step, pullopts, output, pulled)

	_, err := e.client.ContainerCreate(ctx,
		toConfig(pipelineConfig, step),
		toHostConfig(pipelineConfig, step),
		toNetConfig(pipelineConfig, step),
		step.ID,
	)

	// automatically pull and try to re-create the image if the
	// failure is caused because the image does not exist.
	if client.IsErrNotFound(err) && step.Pull != spec.PullNever {
		pullerr := e.pullImageWithRetries(ctx, step.Image, pullopts, output)
		if pullerr != nil {
			return pullerr
		}

		// once the image is successfully pulled we attempt to
		// re-create the container.
		_, err = e.client.ContainerCreate(ctx,
			toConfig(pipelineConfig, step),
			toHostConfig(pipelineConfig, step),
			toNetConfig(pipelineConfig, step),
			step.ID,
		)
	}
	if err != nil {
		return err
	}

	// attach the container to user-defined networks.
	// primarily used to attach global user-defined networks.
	if step.Network == "" {
		for _, net := range step.Networks {
			err = e.client.NetworkConnect(ctx, net, step.ID, &network.EndpointSettings{
				Aliases: []string{net},
			})
			if err != nil {
				return nil
			}
		}
	}

	e.mu.Lock()
	e.containers = append(e.containers, step.ID)
	e.mu.Unlock()

	return nil
}

// helper function emulates the `docker start` command.
func (e *Docker) start(ctx context.Context, id string) error {
	return e.client.ContainerStart(ctx, id, types.ContainerStartOptions{})
}

// helper function emulates the `docker wait` command, blocking
// until the container stops and returning the exit code.
func (e *Docker) waitRetry(ctx context.Context, id string) (*runtime.State, error) {
	for {
		// if the context is canceled, meaning the
		// pipeline timed out or was killed by the
		// end-user, we should exit with an error.
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		state, err := e.wait(ctx, id)
		if err != nil {
			return nil, err
		}
		if state.Exited {
			return state, err
		}
		logger.FromContext(ctx).
			WithField("container", id).
			Trace("docker wait exited unexpectedly")
	}
}

// helper function emulates the `docker wait` command, blocking
// until the container stops and returning the exit code.
func (e *Docker) wait(ctx context.Context, id string) (*runtime.State, error) {
	wait, errc := e.client.ContainerWait(ctx, id, container.WaitConditionNotRunning)
	select {
	case <-wait:
	case <-errc:
	}

	info, err := e.client.ContainerInspect(ctx, id)
	if err != nil {
		return nil, err
	}

	return &runtime.State{
		Exited:    !info.State.Running,
		ExitCode:  info.State.ExitCode,
		OOMKilled: info.State.OOMKilled,
	}, nil
}

// helper function which emulates the docker logs command and writes the log output to
// the writer
func (e *Docker) logs(ctx context.Context, id string, output io.Writer) error {
	opts := types.ContainerLogsOptions{
		Follow:     true,
		ShowStdout: true,
		ShowStderr: true,
		Details:    false,
		Timestamps: false,
	}

	logs, err := e.client.ContainerLogs(ctx, id, opts)
	if err != nil {
		logger.FromContext(ctx).WithError(err).
			WithField("container", id).
			Errorln("failed to stream logs")
		return err
	}
	defer logs.Close()

	_, err = stdcopy.StdCopy(output, output, logs)
	if err != nil {
		logger.FromContext(ctx).WithError(err).
			WithField("container", id).
			Errorln("failed to copy logs from log stream")
	}

	return nil
}

func (e *Docker) pullImage(ctx context.Context, image string, pullOpts types.ImagePullOptions, output io.Writer) error {
	rc, pullerr := e.client.ImagePull(ctx, image, pullOpts)
	if pullerr != nil {
		return pullerr
	}

	if e.hidePull {
		if _, cerr := io.Copy(io.Discard, rc); cerr != nil {
			logrus.WithField("error", cerr).Warnln("failed to discard image pull logs")
		}
	} else {
		if cerr := jsonmessage.Copy(rc, output); cerr != nil {
			logrus.WithField("error", cerr).Warnln("failed to copy image pull logs to output")
		}
	}
	rc.Close()
	return nil
}

func (e *Docker) pullImageWithRetries(ctx context.Context, image string,
	pullOpts types.ImagePullOptions, output io.Writer) error {
	var err error
	for i := 1; i <= imageMaxRetries; i++ {
		err = e.pullImage(ctx, image, pullOpts, output)
		if err == nil {
			return nil
		}
		logrus.WithError(err).
			WithField("image", image).
			Warnln("failed to pull image")

		switch {
		case errdefs.IsNotFound(err),
			errdefs.IsUnauthorized(err),
			errdefs.IsInvalidParameter(err),
			errdefs.IsForbidden(err),
			errdefs.IsCancelled(err),
			errdefs.IsDeadline(err):
			return err
		default:
			if i < imageMaxRetries {
				logrus.WithField("image", image).Infoln("retrying image pull")
			}
		}
		time.Sleep(time.Millisecond * imageRetrySleepDuration)
	}
	return err
}

func (e *Docker) createNetworkWithRetries(ctx context.Context,
	pipelineConfig *spec.PipelineConfig) error {
	// creates the default pod network. All containers
	// defined in the pipeline are attached to this network.
	driver := "bridge"
	if pipelineConfig.Platform.OS == "windows" {
		driver = "nat"
	}

	var err error
	for i := 1; i <= networkMaxRetries; i++ {
		_, err = e.client.NetworkCreate(ctx, pipelineConfig.Network.ID, types.NetworkCreate{
			Driver:  driver,
			Options: pipelineConfig.Network.Options,
			Labels:  pipelineConfig.Network.Labels,
		})
		if err == nil {
			return nil
		}

		time.Sleep(time.Millisecond * networkMaxRetries)
	}
	return err
}

func (e *Docker) pullDockerImage(ctx context.Context, step *spec.Step, pullOpts types.ImagePullOptions, output io.Writer) (bool, error) {
	pulled := false

	// automatically pull the latest version of the image if requested
	// by the process configuration, or if the image is :latest
	if step.Pull == spec.PullAlways ||
		(step.Pull == spec.PullDefault && image.IsLatest(step.Image)) {
		pullerr := e.pullImageWithRetries(ctx, step.Image, pullOpts, output)
		if pullerr != nil {
			return false, pullerr
		}
		pulled = true
	}
	return pulled, nil
}

func (e *Docker) dockerEnvOverride(ctx context.Context, step *spec.Step, pullOpts types.ImagePullOptions, output io.Writer, pulled bool) {
	if path, ok := step.Envs[pathEnvVariable]; ok {
		// pull the image if not already pulled
		if !pulled {
			// check if already present on VM else pull
			pulled = true
			if exists := e.imageExistsLocally(ctx, step.Image); !exists {
				if pullerr := e.pullImageWithRetries(ctx, step.Image, pullOpts, output); pullerr != nil {
					logrus.Warnf("Unable to pull docker image for inspection, failed with err: %s", pullerr)
					pulled = false
				}
			}
		}

		if pulled {
			// inspect the image
			dockerEnvMap := e.getDockerImageEnvs(ctx, step.Image)
			if dockerPathEnv, ok := dockerEnvMap[pathEnvVariable]; ok {
				step.Envs[pathEnvVariable] = strings.TrimRight(path, ":") + ":" + dockerPathEnv
			}
		}
	}
}

func (e *Docker) imageExistsLocally(ctx context.Context, imageName string) bool {
	images, err := e.client.ImageList(ctx, types.ImageListOptions{})
	if err != nil {
		return false
	}
	for i := range images {
		for _, fqn := range images[i].RepoTags {
			if fqn == imageName {
				return true
			}
		}
	}
	return false
}

func (e *Docker) getDockerImageEnvs(ctx context.Context, image string) map[string]string {
	dockerEnvMap := make(map[string]string)
	if imageInspect, _, err := e.client.ImageInspectWithRaw(ctx, image); err == nil {
		for _, envVar := range imageInspect.Config.Env {
			parts := strings.SplitN(envVar, "=", dockerImageSplitLength)
			if len(parts) == dockerImageSplitLength {
				dockerEnvMap[parts[0]] = parts[1]
			}
		}
	} else {
		logrus.Errorf("Unable to inspect docker image, failed with err: %s", err)
	}
	return dockerEnvMap
}
