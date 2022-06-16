// Copyright 2022 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

// Copyright 2019 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package spec

type (

	// PipelineConfig provides the pipeline level configuration valid for all
	// the steps in the pipeline.
	PipelineConfig struct {
		Platform          Platform          `json:"platform,omitempty"`
		Volumes           []*Volume         `json:"volumes,omitempty"`
		Network           Network           `json:"network"`
		Envs              map[string]string `json:"envs,omitempty"`
		Files             []*File           `json:"files,omitempty"`
		EnableDockerSetup *bool             `json:"mount_docker_socket"`
	}

	// Step defines a pipeline step.
	Step struct {
		ID           string            `json:"id,omitempty"`
		Auth         *Auth             `json:"auth,omitempty"`
		Command      []string          `json:"args,omitempty"`
		CPUPeriod    int64             `json:"cpu_period,omitempty"`
		CPUQuota     int64             `json:"cpu_quota,omitempty"`
		CPUShares    int64             `json:"cpu_shares,omitempty"`
		CPUSet       []string          `json:"cpu_set,omitempty"`
		Detach       bool              `json:"detach,omitempty"`
		Devices      []*VolumeDevice   `json:"devices,omitempty"`
		DNS          []string          `json:"dns,omitempty"`
		DNSSearch    []string          `json:"dns_search,omitempty"`
		Entrypoint   []string          `json:"entrypoint,omitempty"`
		Envs         map[string]string `json:"environment,omitempty"`
		ExtraHosts   []string          `json:"extra_hosts,omitempty"`
		IgnoreStdout bool              `json:"ignore_stderr,omitempty"`
		IgnoreStderr bool              `json:"ignore_stdout,omitempty"`
		Image        string            `json:"image,omitempty"`
		Labels       map[string]string `json:"labels,omitempty"`
		MemSwapLimit int64             `json:"memswap_limit,omitempty"`
		MemLimit     int64             `json:"mem_limit,omitempty"`
		Name         string            `json:"name,omitempty"`
		Network      string            `json:"network,omitempty"`
		Networks     []string          `json:"networks,omitempty"`
		PortBindings map[string]string `json:"port_bindings,omitempty"` // Host port to container port mapping.
		Privileged   bool              `json:"privileged,omitempty"`
		Pull         PullPolicy        `json:"pull,omitempty"`
		Secrets      []*Secret         `json:"secrets,omitempty"`
		ShmSize      int64             `json:"shm_size,omitempty"`
		User         string            `json:"user,omitempty"`
		Volumes      []*VolumeMount    `json:"volumes,omitempty"`
		Files        []*File           `json:"files,omitempty"`
		WorkingDir   string            `json:"working_dir,omitempty"`
	}

	// Secret represents a secret variable.
	Secret struct {
		Name string `json:"name,omitempty"`
		Env  string `json:"env,omitempty"`
		Data []byte `json:"data,omitempty"`
		Mask bool   `json:"mask,omitempty"`
	}

	// Platform defines the target platform.
	Platform struct {
		OS      string `json:"os,omitempty"`
		Arch    string `json:"arch,omitempty"`
		Variant string `json:"variant,omitempty"`
		Version string `json:"version,omitempty"`
	}

	// Volume that can be mounted by containers.
	Volume struct {
		EmptyDir *VolumeEmptyDir `json:"temp,omitempty"`
		HostPath *VolumeHostPath `json:"host,omitempty"`
	}

	// files or folder created on the host as part of setup or a step.
	File struct {
		Path  string `json:"path,omitempty"`
		Mode  uint32 `json:"mode,omitempty"`
		Data  string `json:"data,omitempty"`
		IsDir bool   `json:"is_dir,omitempty"`
	}

	// VolumeMount describes a mounting of a Volume
	// within a container.
	VolumeMount struct {
		Name string `json:"name,omitempty"`
		Path string `json:"path,omitempty"`
	}

	// VolumeEmptyDir mounts a temporary directory from the
	// host node's filesystem into the container. This can
	// be used as a shared scratch space.
	VolumeEmptyDir struct {
		ID        string            `json:"id,omitempty"`
		Name      string            `json:"name,omitempty"`
		Medium    string            `json:"medium,omitempty"`
		SizeLimit int64             `json:"size_limit,omitempty"`
		Labels    map[string]string `json:"labels,omitempty"`
	}

	// VolumeHostPath mounts a file or directory from the
	// host node's filesystem into your container.
	VolumeHostPath struct {
		ID       string            `json:"id,omitempty"`
		Name     string            `json:"name,omitempty"`
		Path     string            `json:"path,omitempty"`
		Labels   map[string]string `json:"labels,omitempty"`
		ReadOnly bool              `json:"read_only,omitempty"`
	}

	// VolumeDevice describes a mapping of a raw block
	// device within a container.
	VolumeDevice struct {
		Name       string `json:"name,omitempty"`
		DevicePath string `json:"path,omitempty"`
	}

	// Network that is created and attached to containers
	Network struct {
		ID      string            `json:"id,omitempty"`
		Labels  map[string]string `json:"labels,omitempty"`
		Options map[string]string `json:"options,omitempty"`
	}

	// Auth defines dockerhub authentication credentials.
	Auth struct {
		Address  string `json:"address,omitempty"`
		Username string `json:"username,omitempty"`
		Password string `json:"password,omitempty"`
	}
)
