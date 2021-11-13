package api

import (
	"github.com/harness/lite-engine/engine/spec"
)

type SetupRequest struct {
	Envs      map[string]string `json:"envs,omitempty"`
	Network   spec.Network      `json:"network"`
	Platform  spec.Platform     `json:"platform,omitempty"`
	Volumes   []*spec.Volume    `json:"volumes,omitempty"`
	Secrets   []string          `json:"secrets,omitempty"`
	LogConfig LogConfig         `json:"log_config,omitempty"`
	TIConfig  TIConfig          `json:"ti_config,omitempty"`
}

type SetupResponse struct{}

type DestroyRequest struct{}

type DestroyResponse struct{}

type StartStepRequest struct {
	ID         string            `json:"id,omitempty"` // Unique identifier of step
	Detach     bool              `json:"detach,omitempty"`
	Envs       map[string]string `json:"environment,omitempty"`
	Name       string            `json:"name,omitempty"`
	Secrets    []string          `json:"secrets,omitempty"`
	WorkingDir string            `json:"working_dir,omitempty"`
	Kind       StepType          `json:"kind,omitempty"`
	Run        RunConfig         `json:"run,omitempty"`
	RunTest    RunTestConfig     `json:"run_test,omitempty"`

	LogKey     string   `json:"log_key,omitempty"`
	OutputVars []string `json:"output_var,omitempty"`
	Timeout    int      `json:"timeout,omitempty"` // step timeout in seconds

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
	Privileged   bool                 `json:"privileged,omitempty"`
	Pull         spec.PullPolicy      `json:"pull,omitempty"`
	ShmSize      int64                `json:"shm_size,omitempty"`
	User         string               `json:"user,omitempty"`
	Volumes      []*spec.VolumeMount  `json:"volumes,omitempty"`
}

type StartStepResponse struct{}

type PollStepRequest struct {
	ID string `json:"id,omitempty"`
}

type PollStepResponse struct {
	Exited    bool `json:"exited,omitempty"`
	ExitCode  int  `json:"exit_code,omitempty"`
	OOMKilled bool `json:"oom_killed,omitempty"`
}

type RunConfig struct {
	Command    []string `json:"commands,omitempty"`
	Entrypoint []string `json:"entrypoint,omitempty"`
}

type RunTestConfig struct {
	Args                 string   `json:"args,omitempty"`
	Entrypoint           []string `json:"entrypoint,omitempty"`
	PreCommand           string   `json:"pre_command,omitempty"`
	PostCommand          string   `json:"post_command,omitempty"`
	BuildTool            string   `json:"build_tool,omitempty"`
	Language             string   `json:"language,omitempty"`
	Packages             string   `json:"packages,omitempty"`
	RunOnlySelectedTests bool     `json:"run_only_selected_tests,omitempty"`
	TestAnnotations      string   `json:"test_annotations,omitempty"`
}

type LogConfig struct {
	AccountID      string `json:"account_id,omitempty"`
	IndirectUpload bool   `json:"indirect_upload,omitempty"` // Whether to directly upload via signed link or using log service
	URL            string `json:"url,omitempty"`
	Token          string `json:"token,omitempty"`
}

type TIConfig struct {
	URL   string `json:"url,omitempty"`
	Token string `json:"token,omitempty"`
}
