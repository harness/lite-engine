// Copyright 2022 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

// Copyright 2019 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package docker

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
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
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/volume"
	"github.com/docker/docker/client"
	"github.com/docker/docker/errdefs"
	"github.com/drone/runner-go/logger"
	"github.com/drone/runner-go/pipeline/runtime"
	"github.com/drone/runner-go/registry/auths"
	"github.com/harness/lite-engine/logstream"
)

const (
	imageMaxRetries                  = 3
	imageRetrySleepDuration          = 50
	startContainerRetries            = 10
	startContainerRetrySleepDuration = 5
	networkMaxRetries                = 3
	networkRetrySleepDuration        = 50
	harnessHTTPProxy                 = "HARNESS_HTTP_PROXY"
	harnessHTTPSProxy                = "HARNESS_HTTPS_PROXY"
	harnessNoProxy                   = "HARNESS_NO_PROXY"
	dockerServiceDir                 = "/etc/systemd/system/docker.service.d"
	httpProxyConfFilePath            = dockerServiceDir + "/http-proxy.conf"
	directoryPermission              = 0700
	filePermission                   = 0600
	windowsOS                        = "windows"
	removing                         = "removing"
	running                          = "running"
)

// Opts configures the Docker engine.
type Opts struct {
	HidePull bool
}

// Docker implements a Docker pipeline engine.
type Docker struct {
	client   client.APIClient
	hidePull bool
	mu       sync.Mutex
	// We should refactor this out to upper layers and make this stateless.
	// The Docker engine should just be a simple wrapper around docker which does
	// not keep track of the containers it creates.
	containers []Container
}

type Container struct {
	ID       string
	SoftStop bool
}

// New returns a new engine.
func New(client client.APIClient, opts Opts) *Docker {
	return &Docker{
		client:     client,
		hidePull:   opts.HidePull,
		mu:         sync.Mutex{},
		containers: make([]Container, 0),
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

	if _, ok := pipelineConfig.Envs[harnessHTTPProxy]; ok {
		e.setProxyInDockerDaemon(ctx, pipelineConfig)
	}

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

// DestroyContainersByLabel destroys a pipeline config and cleans up all containers with
// a if specified. This should be used in favor of the old Destroy() which is stateful.
func (e *Docker) DestroyContainersByLabel(
	ctx context.Context,
	pipelineConfig *spec.PipelineConfig,
	labelKey string,
	labelValue string,
) error {
	args := filters.NewArgs()
	if labelKey != "" {
		args.Add("label", fmt.Sprintf("%s=%s", labelKey, labelValue))
	}
	ctrs, err := e.client.ContainerList(ctx, types.ContainerListOptions{
		Filters: args,
		All:     true,
	})
	if err != nil {
		return err
	}
	var containers []Container
	for i := range ctrs {
		containers = append(containers, Container{
			ID: ctrs[i].ID,
		})
	}
	return e.destroyContainers(ctx, pipelineConfig, containers)
}

// destroyContainers is a method which takes in a list of containers and a pipeline environment
// to destroy.
func (e *Docker) destroyContainers(
	ctx context.Context,
	pipelineConfig *spec.PipelineConfig,
	containers []Container,
) error {
	removeOpts := types.ContainerRemoveOptions{
		Force:         true,
		RemoveLinks:   false,
		RemoveVolumes: true,
	}

	// stop all containers
	for _, ctr := range containers {
		if ctr.SoftStop {
			e.softStop(ctx, ctr.ID)
		} else {
			if err := e.client.ContainerKill(ctx, ctr.ID, "9"); err != nil {
				logrus.WithField("container", ctr.ID).WithField("error", err).Warnln("failed to kill container")
			}
		}
	}

	// cleanup all containers
	for _, ctr := range containers {
		if err := e.client.ContainerRemove(ctx, ctr.ID, removeOpts); err != nil {
			logrus.WithField("container", ctr.ID).WithField("error", err).Warnln("failed to remove container")
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

// Destroy the pipeline environment.
func (e *Docker) Destroy(ctx context.Context, pipelineConfig *spec.PipelineConfig) error {
	e.mu.Lock()
	containers := e.containers
	e.mu.Unlock()

	return e.destroyContainers(ctx, pipelineConfig, containers)
}

// Run runs the pipeline step.
func (e *Docker) Run(ctx context.Context, pipelineConfig *spec.PipelineConfig, step *spec.Step,
	output io.Writer, isDrone bool) (*runtime.State, error) {
	// create the container
	err := e.create(ctx, pipelineConfig, step, output)
	if err != nil {
		return nil, errors.TrimExtraInfo(err)
	}
	// start the execution in go routine if it's a detach step and not drone
	if !isDrone && step.Detach {
		go func() {
			ctxBg := context.Background()
			var cancel context.CancelFunc
			if deadline, ok := ctx.Deadline(); ok {
				ctxBg, cancel = context.WithTimeout(ctxBg, time.Until(deadline))
				defer cancel()
			}
			e.startContainer(ctxBg, step.ID, pipelineConfig.TTY, output) //nolint:errcheck
			if wr, ok := output.(logstream.Writer); ok {
				wr.Close()
			}
		}()
		return &runtime.State{Exited: false}, nil
	}
	return e.startContainer(ctx, step.ID, pipelineConfig.TTY, output)
}

func (e *Docker) startContainer(ctx context.Context, stepID string, tty bool, output io.Writer) (*runtime.State, error) {
	// start the container
	startTime := time.Now()
	logrus.WithContext(ctx).Infoln(fmt.Sprintf("Starting command on container for step %s", stepID))
	var err error
	for i := 0; i < startContainerRetries; i++ {
		err = e.start(ctx, stepID)
		if err != nil {
			logrus.WithContext(ctx).WithError(err).Errorln(fmt.Sprintf("Error while starting container for the step %s, retry number %d", stepID, i+1))
			time.Sleep(time.Second * startContainerRetrySleepDuration)
		} else {
			break
		}
	}

	if err != nil {
		return nil, errors.TrimExtraInfo(err)
	}
	// grab the logs from the container execution
	err = e.logs(ctx, stepID, tty, output)
	if err != nil {
		return nil, errors.TrimExtraInfo(err)
	}
	// wait for the response
	state, err := e.waitRetry(ctx, stepID)
	logrus.WithContext(ctx).Infoln(fmt.Sprintf("Completed command on container for step %s, took %.2f seconds", stepID, time.Since(startTime).Seconds()))
	return state, err
}

//
// emulate docker commands
//

func (e *Docker) create(ctx context.Context, pipelineConfig *spec.PipelineConfig, step *spec.Step, output io.Writer) error {
	// create pull options with encoded authorization credentials.
	pullopts := types.ImagePullOptions{}

	// OIDC Authentication
	gcpOidcEnvMapFromStep := step.Envs
	gcpOidcProjectNumber := gcpOidcEnvMapFromStep["PLUGIN_PROJECT_NUMBER"]
	gcpOidcProviderId := gcpOidcEnvMapFromStep["PLUGIN_PROVIDER_ID"]
	gcpOidcPoolId := gcpOidcEnvMapFromStep["PLUGIN_POOL_ID"]
	gcpOidcSA := gcpOidcEnvMapFromStep["PLUGIN_SERVICE_ACCOUNT_EMAIL"]
	gcpOidcToken := gcpOidcEnvMapFromStep["PLUGIN_OIDC_TOKEN_ID"]
	logrus.Infof("OIDC env values: %s, %s, %s, %s, %s", gcpOidcProjectNumber, gcpOidcProviderId, gcpOidcPoolId, gcpOidcSA, gcpOidcToken)
	if gcpOidcProjectNumber != "" && gcpOidcProviderId != "" && gcpOidcPoolId != "" && gcpOidcSA != "" && gcpOidcToken != "" {
		federalToken, err := auths.GetGcpFederalToken(gcpOidcToken, gcpOidcProjectNumber, gcpOidcPoolId, gcpOidcProviderId)
		if err != nil {
			return fmt.Errorf("OIDC token retrieval failed: %w", err)
		}
		logrus.Infof("Generated federal token: %s", federalToken)
		oidcToken, err := auths.GetGoogleCloudAccessToken(federalToken, gcpOidcSA)
		if err != nil {
			return fmt.Errorf("Error getting Google Cloud Access Token: %w", err)
		}
		logrus.Infof("Generated SA OIDC token: %s", oidcToken)
		step.Auth.OidcToken = oidcToken

	}
	if step.Auth != nil {
		pullopts.RegistryAuth = auths.Header(
			step.Auth.Username,
			step.Auth.Password,
			step.Auth.OidcToken,
		)
	}

	// automatically pull the latest version of the image if requested
	// by the process configuration, or if the image is :latest
	if step.Pull == spec.PullAlways ||
		(step.Pull == spec.PullDefault && image.IsLatest(step.Image)) {
		pullerr := e.pullImageWithRetries(ctx, step.Image, pullopts, output)
		if pullerr != nil {
			return pullerr
		}
	}

	containerCreateBody, err := e.client.ContainerCreate(ctx,
		toConfig(pipelineConfig, step),
		toHostConfig(pipelineConfig, step),
		toNetConfig(pipelineConfig, step),
		step.ID,
	)
	if err == nil {
		logrus.WithField("step", step.Name).WithField("body", containerCreateBody).Infoln("Created container for the step")
	}

	// automatically pull and try to re-create the image if the
	// failure is caused because the image does not exist.
	if client.IsErrNotFound(err) && step.Pull != spec.PullNever {
		pullerr := e.pullImageWithRetries(ctx, step.Image, pullopts, output)
		if pullerr != nil {
			return pullerr
		}

		// once the image is successfully pulled we attempt to
		// re-create the container.
		containerCreateBody, err = e.client.ContainerCreate(ctx,
			toConfig(pipelineConfig, step),
			toHostConfig(pipelineConfig, step),
			toNetConfig(pipelineConfig, step),
			step.ID,
		)
		if err == nil {
			logrus.WithField("step", step.Name).WithField("body", containerCreateBody).Infoln("Created container for the step")
		}
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
	e.containers = append(e.containers, Container{
		ID:       step.ID,
		SoftStop: step.SoftStop,
	})
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
func (e *Docker) logs(ctx context.Context, id string, tty bool, output io.Writer) error {
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

	if tty {
		_, err = io.Copy(output, logs)
		if err != nil && err != io.EOF {
			logger.FromContext(ctx).WithError(err).
				WithField("container", id).
				Errorln("failed to copy logs from log stream")
		}
	} else {
		// multiplexed copy of stdout and stderr
		_, err = stdcopy.StdCopy(output, output, logs)
		if err != nil {
			logger.FromContext(ctx).WithError(err).
				WithField("container", id).
				Errorln("failed to copy logs from log stream")
		}
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
			return cerr
		}
	} else {
		if cerr := jsonmessage.Copy(rc, output); cerr != nil {
			logrus.WithField("error", cerr).Warnln("failed to copy image pull logs to output")
			return cerr
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

func (e *Docker) setProxyInDockerDaemon(ctx context.Context, pipelineConfig *spec.PipelineConfig) {
	httpProxy := pipelineConfig.Envs[harnessHTTPProxy]
	httpsProxy := pipelineConfig.Envs[harnessHTTPSProxy]
	noProxy := pipelineConfig.Envs[harnessNoProxy]
	if pipelineConfig.Platform.OS == windowsOS {
		os.Setenv("HTTP_PROXY", httpProxy)
		os.Setenv("HTTPS_PROXY", httpsProxy)
		os.Setenv("NO_PROXY", noProxy)

		// Restart Docker service
		if err := exec.Command("Restart-Service", "docker").Run(); err != nil {
			logger.FromContext(ctx).WithError(err).Infoln("Error restarting Docker service")
			return
		}
	} else {
		if _, err := os.Stat(dockerServiceDir); os.IsNotExist(err) {
			if err := os.MkdirAll(dockerServiceDir, directoryPermission); err != nil {
				logger.FromContext(ctx).WithError(err).Infoln("Unable to create directory for setting proxy in docker daemon")
				return
			}
		}

		proxyConf := fmt.Sprintf(`[Service]
	Environment="HTTP_PROXY=%s"
	Environment="HTTPS_PROXY=%s"
	Environment="NO_PROXY=%s"
	`, httpProxy, httpsProxy, noProxy)

		if err := os.WriteFile(httpProxyConfFilePath, []byte(proxyConf), filePermission); err != nil {
			logger.FromContext(ctx).WithError(err).Infoln("Error writing proxy configuration")
			return
		}

		// Reload systemd daemon
		if err := exec.Command("systemctl", "daemon-reload").Run(); err != nil {
			logger.FromContext(ctx).WithError(err).Infoln("Error reloading systemd daemon")
			return
		}

		// Restart Docker service
		if err := exec.Command("systemctl", "restart", "docker").Run(); err != nil {
			logger.FromContext(ctx).WithError(err).Infoln("Error restarting Docker service")
			return
		}
	}
}

// softStop stops the container giving them a 30 seconds grace period. The signal sent by ContainerStop is SIGTERM.
// After the grace period, the container is killed with SIGKILL.
// After all the containers are stopped, they are removed only when the status is not "running" or "removing".
func (e *Docker) softStop(ctx context.Context, name string) {
	timeout := 30 * time.Second
	if err := e.client.ContainerStop(ctx, name, &timeout); err != nil {
		logrus.WithField("container", name).WithField("error", err).Warnln("failed to stop the container")
	}

	// Before removing the container we want to be sure that it's in a healthy state to be removed.
	now := time.Now()
	for {
		if time.Since(now) > timeout {
			break
		}
		time.Sleep(1 * time.Second)
		containerStatus, err := e.client.ContainerInspect(ctx, name)
		if err != nil {
			logrus.WithField("container", name).WithField("error", err).Warnln("failed to retrieve container stats")
			continue
		}
		if containerStatus.State.Status == removing || containerStatus.State.Status == running {
			continue
		}
		// everything has stopped
		break
	}
}
