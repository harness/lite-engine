// Copyright 2022 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

// Copyright 2019 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package docker

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/harness/lite-engine/engine/spec"
	"github.com/sirupsen/logrus"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/go-connections/nat"
)

const (
	buildCacheStepName = "harness-build-cache"
	annotationsFFEnv   = "CI_ENABLE_HARNESS_ANNOTATIONS"
	hcliPath           = "/usr/bin/hcli"
)

// returns a container configuration.
func toConfig(pipelineConfig *spec.PipelineConfig, step *spec.Step, image string) *container.Config {
	config := &container.Config{
		Image:        image,
		Labels:       step.Labels,
		WorkingDir:   step.WorkingDir,
		User:         step.User,
		AttachStdin:  false,
		AttachStdout: true,
		AttachStderr: true,
		Tty:          pipelineConfig.TTY,
		OpenStdin:    false,
		StdinOnce:    false,
		ArgsEscaped:  false,
	}

	if len(step.Envs) != 0 {
		config.Env = spec.ToEnv(step.Envs)
	}
	for _, sec := range step.Secrets {
		if sec.Env != "" {
			config.Env = append(config.Env, sec.Env+"="+string(sec.Data))
		}
	}

	if len(step.Entrypoint) != 0 {
		config.Entrypoint = step.Entrypoint
	}
	if len(step.Command) != 0 {
		config.Cmd = step.Command
	}
	if len(step.Volumes) != 0 {
		config.Volumes = toVolumeSet(pipelineConfig, step)
	}
	if len(step.PortBindings) != 0 {
		exposedPorts := make(nat.PortSet)
		for _, ctrPort := range step.PortBindings {
			exposedPorts[nat.Port(ctrPort)] = struct{}{}
		}
		config.ExposedPorts = exposedPorts
	}
	return config
}

// returns a container host configuration.
func toHostConfig(pipelineConfig *spec.PipelineConfig, step *spec.Step) *container.HostConfig {
	config := &container.HostConfig{
		LogConfig: container.LogConfig{
			Type: "json-file",
		},
		Privileged: step.Privileged,
		ShmSize:    step.ShmSize,
	}
	// windows does not support privileged so we hard-code
	// this value to false.
	if pipelineConfig.Platform.OS == "windows" {
		config.Privileged = false
	}
	if len(step.Network) > 0 { //nolint:gocritic // emptyStringTest: intentional length check
		config.NetworkMode = container.NetworkMode(step.Network)
	}
	if len(step.DNS) > 0 {
		config.DNS = step.DNS
	}
	if len(step.DNSSearch) > 0 {
		config.DNSSearch = step.DNSSearch
	}
	if len(step.ExtraHosts) > 0 {
		config.ExtraHosts = step.ExtraHosts
	}
	if !isUnlimited(step) {
		config.Resources = container.Resources{
			CPUPeriod:  step.CPUPeriod,
			CPUQuota:   step.CPUQuota,
			CpusetCpus: strings.Join(step.CPUSet, ","),
			CPUShares:  step.CPUShares,
			Memory:     step.MemLimit,
			MemorySwap: step.MemSwapLimit,
		}
	}

	if len(step.Volumes) != 0 {
		config.Devices = toDeviceSlice(pipelineConfig, step)
		config.Binds = toVolumeSlice(pipelineConfig, step)
		config.Mounts = toVolumeMounts(pipelineConfig, step)
	}

	// Mount hcli binary for containers
	if runtime.GOOS == windowsOS {
		// Windows: Mount directory
		config.Mounts = append(config.Mounts, mount.Mount{
			Type:     mount.TypeBind,
			Source:   `C:\Program Files\lite-engine`,
			Target:   `C:\harness\lite-engine`,
			ReadOnly: true,
		})
	} else {
		// Linux/macOS: Determine source path based on OS
		hcliSourcePath := hcliPath
		if runtime.GOOS == "darwin" {
			// macOS: Check $HOME/harness/bin/hcli-linux, fallback to /tmp/harness/bin/hcli-linux
			if home, err := os.UserHomeDir(); err == nil && home != "" {
				hcliSourcePath = filepath.Join(home, "harness", "bin", "hcli-linux")
			} else {
				hcliSourcePath = "/tmp/harness/bin/hcli-linux"
			}
		}
		
		// Common check and mount logic
		if _, err := os.Stat(hcliSourcePath); err == nil {
			config.Mounts = append(config.Mounts, mount.Mount{
				Type:     mount.TypeBind,
				Source:   hcliSourcePath,
				Target:   hcliPath, // Always /usr/bin/hcli inside container
				ReadOnly: true,
			})
		} else {
			logrus.WithField("path", hcliSourcePath).Warnln("hcli binary not found for mounting - annotations may not work in containers")
		}
	}

	if len(step.PortBindings) != 0 {
		portBinding := make(nat.PortMap)
		for hostPort, ctrPort := range step.PortBindings {
			p := nat.Port(ctrPort)
			if _, ok := portBinding[p]; ok {
				portBinding[p] = append(portBinding[p], nat.PortBinding{HostPort: hostPort})
			} else {
				portBinding[p] = []nat.PortBinding{
					{
						HostPort: hostPort,
					},
				}
			}
		}
		config.PortBindings = portBinding
	}
	return config
}

// helper function returns the container network configuration.
func toNetConfig(pipelineConfig *spec.PipelineConfig, proc *spec.Step) *network.NetworkingConfig {
	// if the user overrides the default network we do not
	// attach to the user-defined network.
	if proc.Network != "" && proc.Name != buildCacheStepName {
		return &network.NetworkingConfig{}
	}
	endpoints := map[string]*network.EndpointSettings{}
	fmt.Printf("proc.Name: %s, \nproc.NetworkAliases: %v\n", proc.Name, proc.NetworkAliases)
	endpoints[pipelineConfig.Network.ID] = &network.EndpointSettings{
		NetworkID: pipelineConfig.Network.ID,
		Aliases:   append([]string{proc.Name}, proc.NetworkAliases...),
	}
	return &network.NetworkingConfig{
		EndpointsConfig: endpoints,
	}
}

// helper function that converts a slice of device paths to a slice of
// container.DeviceMapping.
func toDeviceSlice(pipelineConfig *spec.PipelineConfig, step *spec.Step) []container.DeviceMapping {
	var to []container.DeviceMapping
	for _, mount := range step.Devices {
		device, ok := lookupVolume(pipelineConfig, mount.Name)
		if !ok {
			continue
		}
		if !isDevice(device) {
			continue
		}
		to = append(to, container.DeviceMapping{
			PathOnHost:        device.HostPath.Path,
			PathInContainer:   mount.DevicePath,
			CgroupPermissions: "rwm",
		})
	}
	if len(to) == 0 {
		return nil
	}
	return to
}

// helper function that converts a slice of volume paths to a set
// of unique volume names.
func toVolumeSet(pipelineConfig *spec.PipelineConfig, step *spec.Step) map[string]struct{} {
	set := map[string]struct{}{}
	for _, mount := range step.Volumes {
		volume, ok := lookupVolume(pipelineConfig, mount.Name)
		if !ok {
			continue
		}
		if isDevice(volume) {
			continue
		}
		if isNamedPipe(volume) {
			continue
		}
		if !isBindMount(volume) {
			continue
		}
		set[mount.Path] = struct{}{}
	}
	return set
}

// helper function returns a slice of volume mounts.
func toVolumeSlice(pipelineConfig *spec.PipelineConfig, step *spec.Step) []string {
	// this entire function should be deprecated in
	// favor of toVolumeMounts, however, I am unable
	// to get it working with data volumes.
	var to []string
	for _, mount := range step.Volumes {
		volume, ok := lookupVolume(pipelineConfig, mount.Name)
		if !ok {
			continue
		}
		if isDevice(volume) {
			continue
		}
		if isDataVolume(volume) {
			path := volume.EmptyDir.ID + ":" + mount.Path
			to = append(to, path)
		}
		if isBindMount(volume) {
			// Here, we attempt to convert any possible symlinks to the actual folder it is pointing to,
			// in order to avoid any issues with vm's filesystem not in sync with symlinks in the host.
			// Known case: Rancher Desktop, in MacOS, uses lima-vm, and the `/tmp` folder in the vm is
			// not the same as the `/tmp` folder of the host (which is a symlink pointing to "/private/tmp").
			// By resolving the symlink here, `/tmp` will be converted to its resolved folder, `/private/tmp`,
			// and the bind mount will work as expected.
			hostPath, err := filepath.EvalSymlinks(volume.HostPath.Path)
			if err != nil {
				// If failed to resolve symlinks, use the original hostPath.
				hostPath = volume.HostPath.Path
			}
			path := hostPath + ":" + mount.Path
			to = append(to, path)
		}
	}
	return to
}

// helper function returns a slice of docker mount
// configurations.
func toVolumeMounts(pipelineConfig *spec.PipelineConfig, step *spec.Step) []mount.Mount {
	var mounts []mount.Mount
	for _, target := range step.Volumes {
		source, ok := lookupVolume(pipelineConfig, target.Name)
		if !ok {
			continue
		}

		if isBindMount(source) && !isDevice(source) {
			continue
		}

		// HACK: this condition can be removed once
		// toVolumeSlice has been fully replaced. at this
		// time, I cannot figure out how to get mounts
		// working with data volumes :(
		if isDataVolume(source) {
			continue
		}
		mounts = append(mounts, toMount(source, target))
	}
	if len(mounts) == 0 {
		return nil
	}
	return mounts
}

// helper function converts the volume declaration to a
// docker mount structure.
func toMount(source *spec.Volume, target *spec.VolumeMount) mount.Mount {
	to := mount.Mount{
		Target: target.Path,
		Type:   toVolumeType(source),
	}
	if isBindMount(source) || isNamedPipe(source) {
		to.Source = source.HostPath.Path
		to.ReadOnly = source.HostPath.ReadOnly
	}
	if isTempfs(source) {
		to.TmpfsOptions = &mount.TmpfsOptions{
			SizeBytes: source.EmptyDir.SizeLimit,
			Mode:      0700, //nolint:mnd
		}
	}
	return to
}

// helper function returns the docker volume enumeration
// for the given volume.
func toVolumeType(from *spec.Volume) mount.Type {
	switch {
	case isDataVolume(from):
		return mount.TypeVolume
	case isTempfs(from):
		return mount.TypeTmpfs
	case isNamedPipe(from):
		return mount.TypeNamedPipe
	default:
		return mount.TypeBind
	}
}

// returns true if the container has no resource limits.
func isUnlimited(res *spec.Step) bool {
	return len(res.CPUSet) == 0 &&
		res.CPUPeriod == 0 &&
		res.CPUQuota == 0 &&
		res.CPUShares == 0 &&
		res.MemLimit == 0 &&
		res.MemSwapLimit == 0
}

// returns true if the volume is a bind mount.
func isBindMount(volume *spec.Volume) bool {
	return volume.HostPath != nil
}

// returns true if the volume is in-memory.
func isTempfs(volume *spec.Volume) bool {
	return volume.EmptyDir != nil && volume.EmptyDir.Medium == "memory" //nolint:goconst
}

// returns true if the volume is a data-volume.
func isDataVolume(volume *spec.Volume) bool {
	return volume.EmptyDir != nil && volume.EmptyDir.Medium != "memory"
}

// returns true if the volume is a device
func isDevice(volume *spec.Volume) bool {
	return volume.HostPath != nil && strings.HasPrefix(volume.HostPath.Path, "/dev/")
}

// returns true if the volume is a named pipe.
func isNamedPipe(volume *spec.Volume) bool {
	return volume.HostPath != nil &&
		strings.HasPrefix(volume.HostPath.Path, `\\.\pipe\`)
}

// helper function returns the named volume.
func lookupVolume(pipelineConfig *spec.PipelineConfig, name string) (*spec.Volume, bool) {
	for _, v := range pipelineConfig.Volumes {
		if v.HostPath != nil && v.HostPath.Name == name {
			return v, true
		}
		if v.EmptyDir != nil && v.EmptyDir.Name == name {
			return v, true
		}
	}
	return nil, false
}

// func toPlatform(pipelineConfig *spec.PipelineConfig) *imagespecs.Platform {
// 	return &imagespecs.Platform{
// 		Architecture: pipelineConfig.Platform.Arch,
// 		OS:           pipelineConfig.Platform.OS,
// 		Variant:      pipelineConfig.Platform.Variant,
// 	}
// }
