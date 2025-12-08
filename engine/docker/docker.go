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

	dockerimage "github.com/harness/lite-engine/engine/docker/image"
	"github.com/harness/lite-engine/engine/spec"
	"github.com/harness/lite-engine/internal/docker/errors"
	"github.com/harness/lite-engine/internal/docker/jsonmessage"
	"github.com/harness/lite-engine/internal/docker/stdcopy"
	"github.com/harness/lite-engine/internal/safego"
	"github.com/sirupsen/logrus"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
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
	harnessHTTPSProxy                = "HARNESS_HTTPS_PROXY"
	harnessNoProxy                   = "HARNESS_NO_PROXY"
	dockerServiceDir                 = "/etc/systemd/system/docker.service.d"
	httpProxyConfFilePath            = dockerServiceDir + "/http-proxy.conf"
	directoryPermission              = 0700
	filePermission                   = 0600
	windowsOS                        = "windows"
	removing                         = "removing"
	running                          = "running"
	DisableAPINegotiation            = "DOCKER_DISABLE_API_NEGOTIATION"
)

// Opts configures the Docker engine.
type Opts struct {
	HidePull bool
	// Callers can pass a non-nil client.APIClient here, and it
	// will be used instead of creating a new docker client
	DockerClient client.APIClient
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
	var cli client.APIClient
	disableAPINegotiation := os.Getenv(DisableAPINegotiation)
	if disableAPINegotiation == "" {
		disableAPINegotiation = "false"
	}

	if opts.DockerClient != nil {
		cli = opts.DockerClient
	} else {
		var err error
		if disableAPINegotiation == "false" {
			cli, err = client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
		} else {
			cli, err = client.NewClientWithOpts(client.FromEnv)
		}

		if err != nil {
			return nil, err
		}
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

	if _, ok := pipelineConfig.Envs[harnessHTTPSProxy]; ok {
		e.setProxyInDockerDaemon(ctx, pipelineConfig)
	}

	for _, vol := range pipelineConfig.Volumes {
		if vol.EmptyDir == nil {
			continue
		}
		_, err := e.client.VolumeCreate(ctx, volume.CreateOptions{
			Name:   vol.EmptyDir.ID,
			Driver: "local",
			Labels: vol.EmptyDir.Labels,
		})
		if err != nil {
			return errors.TrimExtraInfo(err)
		}
	}

	err := e.createNetworkWithRetries(ctx, pipelineConfig)
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
	ctrs, err := e.client.ContainerList(ctx, container.ListOptions{
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
	removeOpts := container.RemoveOptions{
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
				logrus.WithContext(ctx).WithField("container", ctr.ID).WithField("error", err).Warnln("failed to kill container")
			}
		}
	}

	// cleanup all containers
	for _, ctr := range containers {
		if err := e.client.ContainerRemove(ctx, ctr.ID, removeOpts); err != nil {
			logrus.WithContext(ctx).WithField("container", ctr.ID).WithField("error", err).Warnln("failed to remove container")
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
			logrus.WithContext(ctx).WithField("volume", vol.EmptyDir.ID).WithField("error", err).Warnln("failed to remove volume")
		}
	}

	// cleanup the network
	if err := e.client.NetworkRemove(ctx, pipelineConfig.Network.ID); err != nil {
		logrus.WithContext(ctx).WithField("network", pipelineConfig.Network.ID).WithField("error", err).Warnln("failed to remove network")
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
	output io.Writer, isDrone bool, isHosted bool) (*runtime.State, error) {
	// create the container
	err := e.create(ctx, pipelineConfig, step, output, isHosted)
	if err != nil {
		return nil, errors.TrimExtraInfo(err)
	}
	// start the execution in go routine if it's a detach step and not drone
	if !isDrone && step.Detach {
		safego.WithContext(ctx, "detached_container", func(ctx context.Context) {
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
		})
		return &runtime.State{Exited: false}, nil
	}
	return e.startContainer(ctx, step.ID, pipelineConfig.TTY, output)
}

func (e *Docker) Suspend(ctx context.Context, labels map[string]string) error {
	return e.destroyStoppedContainers(ctx, labels)
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

func (e *Docker) destroyStoppedContainers(ctx context.Context, labels map[string]string) error {
	// Create filter to match containers with the given label
	filterArgs := filters.NewArgs()
	for key, value := range labels {
		filterArgs.Add("label", fmt.Sprintf("%s=%s", key, value))
	}
	// Filter only stopped containers
	filterArgs.Add("status", "exited")

	stoppedPluginContainers, err := e.client.ContainerList(ctx, container.ListOptions{
		Filters: filterArgs,
		All:     true, // Required to include stopped containers
	})
	if err != nil {
		return fmt.Errorf("failed to list stopped plugin containers: %w", err)
	}

	for i := range stoppedPluginContainers {
		pluginContainer := stoppedPluginContainers[i]
		if err := e.client.ContainerRemove(ctx, pluginContainer.ID, container.RemoveOptions{}); err != nil {
			logrus.WithContext(ctx).
				WithField("container", pluginContainer.ID).
				WithField("error", err).Warnln("failed to remove container")
		}
		// remove container from e.containers matching container.ID
		e.removeContainerByID(pluginContainer.ID)
	}
	return nil
}

//
// emulate docker commands
//

func (e *Docker) create(ctx context.Context, pipelineConfig *spec.PipelineConfig, step *spec.Step, output io.Writer, isHosted bool) error { //nolint:gocyclo
	// create pull options with encoded authorization credentials.
	pullopts := image.PullOptions{}
	if step.Auth != nil {
		pullopts.RegistryAuth = auths.Header(
			step.Auth.Username,
			step.Auth.Password,
		)
	}

	originalImage := step.Image
	overriddenImage := originalImage

	// override image registry for internal images
	// this is short term solution
	// override to gar if no auth is present
	if isHosted && (step.Auth == nil || step.Auth.Username == "" || step.Auth.Password == "") {
		overriddenImage = dockerimage.OverrideRegistry(step.Image, os.Getenv(spec.CloudDriver))
	}

	selectedImage := overriddenImage

	// automatically pull the latest version of the image if requested
	// by the process configuration, or if the image is :latest
	if step.Pull == spec.PullAlways ||
		(step.Pull == spec.PullDefault && dockerimage.IsLatest(overriddenImage)) {
		pullerr := e.pullImageWithRetries(ctx, overriddenImage, pullopts, output)
		if pullerr != nil {
			// if for some reason overridden image does not work then fallback
			if overriddenImage != originalImage {
				selectedImage = originalImage
				pullerr = e.pullImageWithRetries(ctx, originalImage, pullopts, output)
			}
			if pullerr != nil {
				return pullerr
			}
		}
	}

	containerCreateBody, err := e.client.ContainerCreate(ctx,
		toConfig(pipelineConfig, step, selectedImage),
		toHostConfig(pipelineConfig, step),
		toNetConfig(pipelineConfig, step),
		nil,
		step.ID,
	)
	if err == nil {
		logrus.WithContext(ctx).WithField("step", step.Name).WithField("body", containerCreateBody).Infoln("Created container for the step")
	}

	// automatically pull and try to re-create the image if the
	// failure is caused because the image does not exist.
	if client.IsErrNotFound(err) && step.Pull != spec.PullNever {
		pullerr := e.pullImageWithRetries(ctx, overriddenImage, pullopts, output)
		if pullerr != nil {
			// if for some reason overridden image does not work then fallback
			if overriddenImage != originalImage {
				selectedImage = originalImage
				pullerr = e.pullImageWithRetries(ctx, originalImage, pullopts, output)
			}
			if pullerr != nil {
				return pullerr
			}
		}

		// once the image is successfully pulled we attempt to
		// re-create the container.
		containerCreateBody, err = e.client.ContainerCreate(ctx,
			toConfig(pipelineConfig, step, selectedImage),
			toHostConfig(pipelineConfig, step),
			toNetConfig(pipelineConfig, step),
			nil,
			step.ID,
		)
		if err == nil {
			logrus.WithContext(ctx).WithField("step", step.Name).WithField("body", containerCreateBody).Infoln("Created container for the step")
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
	return e.client.ContainerStart(ctx, id, container.StartOptions{})
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
	opts := container.LogsOptions{
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

func (e *Docker) pullImage(ctx context.Context, img string, pullOpts image.PullOptions, output io.Writer) error {
	rc, pullerr := e.client.ImagePull(ctx, img, pullOpts)
	if pullerr != nil {
		return pullerr
	}

	if e.hidePull {
		if _, cerr := io.Copy(io.Discard, rc); cerr != nil {
			logrus.WithContext(ctx).WithField("error", cerr).Warnln("failed to discard image pull logs")
			return cerr
		}
	} else {
		if cerr := jsonmessage.Copy(rc, output); cerr != nil {
			logrus.WithContext(ctx).WithField("error", cerr).Warnln("failed to copy image pull logs to output")
			return cerr
		}
	}
	rc.Close()
	return nil
}

func (e *Docker) pullImageWithRetries(ctx context.Context, img string,
	pullOpts image.PullOptions, output io.Writer) error {
	var err error
	for i := 1; i <= imageMaxRetries; i++ {
		err = e.pullImage(ctx, img, pullOpts, output)
		if err == nil {
			return nil
		}
		logrus.WithContext(ctx).WithError(err).
			WithField("image", img).
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
				logrus.WithContext(ctx).WithField("image", img).Infoln("retrying image pull")
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

	// Check if the network already exists
	_, _, err := e.client.NetworkInspectWithRaw(ctx, pipelineConfig.Network.ID, network.InspectOptions{})
	if err == nil {
		return nil
	}

	for i := 1; i <= networkMaxRetries; i++ {
		_, err = e.client.NetworkCreate(ctx, pipelineConfig.Network.ID, network.CreateOptions{
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
	httpsProxy := pipelineConfig.Envs[harnessHTTPSProxy]
	noProxy := pipelineConfig.Envs[harnessNoProxy]
	if pipelineConfig.Platform.OS == windowsOS {
		os.Setenv("HTTP_PROXY", httpsProxy)
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
	`, httpsProxy, httpsProxy, noProxy)

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
	timeoutSeconds := 30
	if err := e.client.ContainerStop(ctx, name, container.StopOptions{Timeout: &timeoutSeconds}); err != nil {
		logrus.WithContext(ctx).WithField("container", name).WithField("error", err).Warnln("failed to stop the container")
	}

	// Before removing the container we want to be sure that it's in a healthy state to be removed.
	now := time.Now()
	timeout := 30 * time.Second //nolint:mnd
	for {
		if time.Since(now) > timeout {
			break
		}
		time.Sleep(1 * time.Second)
		containerStatus, err := e.client.ContainerInspect(ctx, name)
		if err != nil {
			logrus.WithContext(ctx).WithField("container", name).WithField("error", err).Warnln("failed to retrieve container stats")
			continue
		}
		if containerStatus.State.Status == removing || containerStatus.State.Status == running {
			continue
		}
		// everything has stopped
		break
	}
}

func (e *Docker) removeContainerByID(id string) {
	newContainers := e.containers[:0]
	for _, c := range e.containers {
		if c.ID != id {
			newContainers = append(newContainers, c)
		}
	}
	e.containers = newContainers
}
