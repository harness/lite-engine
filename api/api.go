// Copyright 2022 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package api

import (
	"github.com/harness/lite-engine/engine/spec"
	"github.com/harness/ti-client/types"
)

type (
	HealthResponse struct {
		Version string `json:"version"`
		OK      bool   `json:"ok"`
	}

	SetupRequest struct {
		Envs              map[string]string  `json:"envs,omitempty"`
		Network           spec.Network       `json:"network"`
		Volumes           []*spec.Volume     `json:"volumes,omitempty"`
		Secrets           []string           `json:"secrets,omitempty"`
		LogConfig         LogConfig          `json:"log_config,omitempty"`
		TIConfig          TIConfig           `json:"ti_config,omitempty"`
		Files             []*spec.File       `json:"files,omitempty"`
		MountDockerSocket *bool              `json:"mount_docker_socket,omitempty"`
		TTY               bool               `json:"tty,omitempty" default:"false"`
		MtlsConfig        spec.MtlsConfig    `json:"mtls_config,omitempty"`
		VMImageConfig     spec.VMImageConfig `json:"vm_image_config,omitempty"`
	}

	SetupResponse struct{}

	SuspendRequest struct {
		LogKey         string            `json:"log_key,omitempty"` // key to write the lite engine logs (optional)
		Labels         map[string]string `json:"labels,omitempty"`
		LiteEnginePath string            `json:"lite_engine_path,omitempty"`
	}

	SuspendResponse struct{}

	DestroyRequest struct {
		LogDrone       bool   `json:"log_drone,omitempty"`
		LogKey         string `json:"log_key,omitempty"`          // key to write the lite engine logs (optional)
		LiteEnginePath string `json:"lite_engine_path,omitempty"` // where to find the lite engine logs
		StageRuntimeID string `json:"stage_runtime_id,omitempty"`
	}

	DestroyResponse struct {
		OSStats *spec.OSStats `json:"os_stats,omitempty"`
	}

	StartStepRequest struct {
		ID             string            `json:"id,omitempty"` // Unique identifier of step
		StageRuntimeID string            `json:"stage_runtime_id,omitempty"`
		Detach         bool              `json:"detach,omitempty"`
		Envs           map[string]string `json:"environment,omitempty"`
		Name           string            `json:"name,omitempty"`
		LogKey         string            `json:"log_key,omitempty"`
		LogDrone       bool              `json:"log_drone"`
		Secrets        []string          `json:"secrets,omitempty"`
		WorkingDir     string            `json:"working_dir,omitempty"`
		Kind           StepType          `json:"kind,omitempty"`
		Run            RunConfig         `json:"run,omitempty"`
		RunTest        RunTestConfig     `json:"run_test,omitempty"`
		RunTestsV2     RunTestsV2Config  `json:"run_test_v2,omitempty"`
		SoftStop       bool              `json:"soft_stop,omitempty"`

		// Configs for log service and test intelligence (currently provided in setup and maintained as state)
		// TODO (Vistaar): LogConfig might be moved out from here.
		LogConfig  LogConfig       `json:"log_config,omitempty"`
		TIConfig   TIConfig        `json:"ti_config,omitempty"`
		MtlsConfig spec.MtlsConfig `json:"mtls_config,omitempty"`

		OutputVars        []string    `json:"output_vars,omitempty"`
		TestReport        TestReport  `json:"test_report,omitempty"`
		Timeout           int         `json:"timeout,omitempty"` // step timeout in seconds
		MountDockerSocket *bool       `json:"mount_docker_socket"`
		Outputs           []*OutputV2 `json:"outputs,omitempty"`

		// File to read from to fetch output variables. Note: If this is set, we ignore
		// output_vars and instead read directly from the file to fetch output variables.
		OutputVarFile string `json:"output_var_file,omitempty"`

		// File to read from to fetch output secret variables
		SecretVarFile string `json:"secret_var_file,omitempty"`

		// Directory to be used as a scratch space for plugins. Technically, we could populate the env
		// variable directly by the user of this library but this is more explicit and gives us more control
		// over the env variable that we expose to plugins.
		ScratchDir string `json:"scratch_dir,omitempty"`

		// Valid only for steps running on docker container
		Auth         *spec.Auth           `json:"auth,omitempty"`
		CPUPeriod    int64                `json:"cpu_period,omitempty"`
		CPUQuota     int64                `json:"cpu_quota,omitempty"`
		CPUShares    int64                `json:"cpu_shares,omitempty"`
		CPUSet       []string             `json:"cpu_set,omitempty"`
		Devices      []*spec.VolumeDevice `json:"devices,omitempty"`
		DNS          []string             `json:"dns,omitempty"`
		DNSSearch    []string             `json:"dns_search,omitempty"`
		ExtraHosts   []string             `json:"extra_hosts,omitempty"`
		IgnoreStdout bool                 `json:"ignore_stderr,omitempty"`
		IgnoreStderr bool                 `json:"ignore_stdout,omitempty"`
		Image        string               `json:"image,omitempty"`
		Labels       map[string]string    `json:"labels,omitempty"`
		MemSwapLimit int64                `json:"memswap_limit,omitempty"`
		MemLimit     int64                `json:"mem_limit,omitempty"`
		Network      string               `json:"network,omitempty"`
		Networks     []string             `json:"networks,omitempty"`
		PortBindings map[string]string    `json:"port_bindings,omitempty"` // Host port to container port mapping
		Privileged   bool                 `json:"privileged,omitempty"`
		Pull         spec.PullPolicy      `json:"pull,omitempty"`
		ShmSize      int64                `json:"shm_size,omitempty"`
		User         string               `json:"user,omitempty"`
		Volumes      []*spec.VolumeMount  `json:"volumes,omitempty"`
		Files        []*spec.File         `json:"files,omitempty"`
		StepStatus   StepStatusConfig     `json:"step_status,omitempty"`
	}

	OutputV2 struct {
		Key   string     `json:"key,omitempty"`
		Value string     `json:"value"`
		Type  OutputType `json:"type,omitempty"`
	}

	StartStepResponse struct{}

	PollStepRequest struct {
		ID string `json:"id,omitempty"`
	}

	PollStepResponse struct {
		Exited            bool                 `json:"exited,omitempty"`
		ExitCode          int                  `json:"exit_code,omitempty"`
		Error             string               `json:"error,omitempty"`
		OOMKilled         bool                 `json:"oom_killed,omitempty"`
		Outputs           map[string]string    `json:"outputs,omitempty"`
		Envs              map[string]string    `json:"envs,omitempty"` // Env variables exported by step
		Artifact          []byte               `json:"artifact,omitempty"`
		OutputV2          []*OutputV2          `json:"outputV2,omitempty"`
		OptimizationState string               `json:"optimization_state,omitempty"`
		TelemetryData     *types.TelemetryData `json:"telemetry_data,omitempty"`
	}

	StreamOutputRequest struct {
		ID     string `json:"id,omitempty"`
		Offset int    `json:"offset,omitempty"`
	}

	RunConfig struct {
		Command    []string `json:"commands,omitempty"`
		Entrypoint []string `json:"entrypoint,omitempty"`
	}

	RunTestsV2Config struct {
		Command          []string `json:"commands,omitempty"`
		Entrypoint       []string `json:"entrypoint,omitempty"`
		TestGlobs        []string `json:"test_globs,omitempty"`
		IntelligenceMode bool     `json:"intelligence_mode,omitempty"`
	}

	RunTestConfig struct {
		Args                 string   `json:"args,omitempty"`
		Entrypoint           []string `json:"entrypoint,omitempty"`
		PreCommand           string   `json:"pre_command,omitempty"`
		PostCommand          string   `json:"post_command,omitempty"`
		BuildTool            string   `json:"build_tool,omitempty"`
		Language             string   `json:"language,omitempty"`
		Packages             string   `json:"packages,omitempty"`
		RunOnlySelectedTests bool     `json:"run_only_selected_tests,omitempty"`
		TestAnnotations      string   `json:"test_annotations,omitempty"`
		TestSplitStrategy    string   `json:"test_split_strategy,omitempty"`
		ParallelizeTests     bool     `json:"parallelize_tests,omitempty"`
		TestGlobs            string   `json:"test_globs,omitempty"`
	}

	LogConfig struct {
		AccountID         string `json:"account_id,omitempty"`
		IndirectUpload    bool   `json:"indirect_upload,omitempty"` // Whether to directly upload via signed link or using log service
		URL               string `json:"url,omitempty"`
		Token             string `json:"token,omitempty"`
		TrimNewLineSuffix bool   `json:"trim_new_line_suffix,omitempty"`
		// Note: setting `skipOpeningStream` as `true` has the effect of making the `Writer` in
		// livelog.go use a stream snapshot to save the final logs, instead of calling `upload`.
		// There is a limit of 5k lines for log-service's snapshot, so this parameter should NOT
		// be used in cases where more than 5k lines of logs are written by the logger. Otherwise,
		// the final logs blob may have missing logs.
		SkipOpeningStream bool `json:"skip_opening_stream,omitempty"`
	}

	TIConfig struct {
		URL          string `json:"url,omitempty"`
		Token        string `json:"token,omitempty"`
		AccountID    string `json:"account_id,omitempty"`
		OrgID        string `json:"org_id,omitempty"`
		ProjectID    string `json:"project_id,omitempty"`
		PipelineID   string `json:"pipeline_id,omitempty"`
		StageID      string `json:"stage_id,omitempty"`
		BuildID      string `json:"build_id,omitempty"`
		Repo         string `json:"repo,omitempty"`
		Sha          string `json:"sha,omitempty"`
		SourceBranch string `json:"source_branch,omitempty"`
		TargetBranch string `json:"target_branch,omitempty"`
		CommitBranch string `json:"commit_branch,omitempty"`
		CommitLink   string `json:"commit_link,omitempty"`
		ParseSavings bool   `json:"parse_savings,omitempty"`
	}

	TestReport struct {
		Kind  ReportType  `json:"kind,omitempty"`
		Junit JunitReport `json:"junit,omitempty"`
	}

	JunitReport struct {
		Paths []string `json:"paths,omitempty"`
	}

	StepStatusConfig struct {
		Endpoint       string `json:"endpoint,omitempty"`
		Token          string `json:"token,omitempty"`
		AccountID      string `json:"account_id,omitempty"`
		DelegateID     string `json:"delegate_id,omitempty"`
		TaskID         string `json:"task_id,omitempty"`
		RunnerResponse bool   `json:"runner_response,omitempty"`
		TaskStatusV2   bool   `json:"task_status_v2,omitempty"`
	}

	VMTaskExecutionResponse struct {
		ErrorMessage           string                 `json:"error_message,omitempty"`
		OutputVars             map[string]string      `json:"output_vars,omitempty"`
		CommandExecutionStatus CommandExecutionStatus `json:"command_execution_status,omitempty"`
		Artifact               []byte                 `json:"artifact,omitempty"`
		Outputs                []*OutputV2            `json:"outputs,omitempty"`
		OptimizationState      string                 `json:"optimization_state,omitempty"`
		TelemetryData          *types.TelemetryData   `json:"telemetry_data,omitempty"`
	}
)

type CommandExecutionStatus string

const (
	Success CommandExecutionStatus = "SUCCESS"
	Failure CommandExecutionStatus = "FAILURE"
	Timeout CommandExecutionStatus = "TIMEOUT"
)

type OutputType string

const (
	OutputTypeString OutputType = "STRING"
	OutputTypeSecret OutputType = "SECRET"
)
