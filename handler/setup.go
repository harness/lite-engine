// Copyright 2022 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/harness/lite-engine/api"
	"github.com/harness/lite-engine/duallog"
	"github.com/harness/lite-engine/engine"
	"github.com/harness/lite-engine/engine/spec"
	"github.com/harness/lite-engine/livelog"
	"github.com/harness/lite-engine/logger"
	"github.com/harness/lite-engine/logstream"
	"github.com/harness/lite-engine/osstats"
	"github.com/harness/lite-engine/pipeline"
	tiCfg "github.com/harness/lite-engine/ti/config"
	"github.com/sirupsen/logrus"
)

var (
	statsInterval          = 30 * time.Second
	harnessEnableDebugLogs = "HARNESS_ENABLE_DEBUG_LOGS"
)

const (
	OSWindows         = "windows"
	dualLoggingEnvVar = "HARNESS_LOG_STREAMING_STDOUT_ENABLED"
)

func GetNetrc(os string) string {
	switch os {
	case OSWindows:
		return "_netrc"
	default:
		return ".netrc"
	}
}

func GetNetrcFile(env map[string]string) (*spec.File, error) {
	netrcName := GetNetrc(runtime.GOOS)
	homeDir, err := os.UserHomeDir()
	if err != nil {
		fmt.Printf("Error getting home directory: %v\n", err)
		return nil, err
	}

	path := filepath.Join(homeDir, netrcName)

	data := fmt.Sprintf("machine %s\nlogin %s\npassword %s\n", env["DRONE_NETRC_MACHINE"], env["DRONE_NETRC_USERNAME"], env["DRONE_NETRC_PASSWORD"])

	return &spec.File{
		Path:  path,
		Mode:  777, //nolint:mnd
		IsDir: false,
		Data:  data,
	}, nil
}

// HandleExecuteStep returns an http.HandlerFunc that executes a step
func HandleSetup(engine *engine.Engine) http.HandlerFunc { //nolint:gocyclo,funlen
	return func(w http.ResponseWriter, r *http.Request) {
		st := time.Now()

		var s api.SetupRequest
		err := json.NewDecoder(r.Body).Decode(&s)
		if err != nil {
			WriteBadRequest(w, err)
			return
		}
		logProcess := false
		if val, ok := s.Envs[harnessEnableDebugLogs]; ok && val == "true" {
			logProcess = true
		}
		collector := osstats.New(context.Background(), statsInterval, logProcess)

		setProxyEnvs(s.Envs)
		setHarnessEnvs(s.Envs)

		if val, ok := s.Envs[dualLoggingEnvVar]; ok && val == "true" {
			s.LogConfig.DualLoggingEnabled = true
		}

		state := pipeline.GetState()
		state.Set(s.Secrets, s.LogConfig, getTiCfg(&s.TIConfig, &s.MtlsConfig, s.Envs), s.MtlsConfig, collector)

		// Initialize lite-engine log streaming if LELogKey is provided
		if err := initializeLELogStreaming(&s, state); err != nil {
			logger.FromRequest(r).
				WithField("time", time.Now().Format(time.RFC3339)).
				WithError(err).
				Warnln("api: failed to initialize lite-engine log streaming")
		}

		if s.LogConfig.DualLoggingEnabled {
			initializeDualLogHook(&s)
		}

		// Initialize OS stats NDJSON streaming (file + upload) if MemoryMetricsLogKey is provided
		if err := initializeOSStatsStreaming(&s, state); err != nil {
			logger.FromRequest(r).
				WithField("time", time.Now().Format(time.RFC3339)).
				WithError(err).
				Warnln("api: failed to initialize os stats streaming")
		}

		if s.MountDockerSocket == nil || *s.MountDockerSocket { // required to support m1 where docker isn't installed.
			s.Volumes = append(s.Volumes, getDockerSockVolume())
		}
		s.Volumes = append(s.Volumes, getSharedVolume())

		if val, ok := s.Envs["DRONE_PERSIST_CREDS"]; ok && val == "true" {
			netrcFile, err := GetNetrcFile(s.Envs)
			if err != nil {
				fmt.Printf("Skipping netrc file creation: %v\n", err)
			} else {
				s.Files = append(s.Files, netrcFile)
			}
		}

		cfg := &spec.PipelineConfig{
			Envs:    s.Envs,
			Network: s.Network,
			Platform: spec.Platform{
				OS:   runtime.GOOS,
				Arch: runtime.GOARCH,
			},
			Volumes:           s.Volumes,
			Files:             s.Files,
			EnableDockerSetup: s.MountDockerSocket,
			TTY:               s.TTY,
			MtlsConfig:        s.MtlsConfig,
			SanitizeConfig:    s.LogConfig.SanitizeConfig,
		}
		collector.Start()

		// Fix clock skew on Linux ARM64 VMs after hibernate resume.
		// ARM64 uses arch_sys_counter which doesn't auto-adjust after GCP suspend/resume.
		// Restarting chrony resets its source state and with makestep 1 -1 config, it steps immediately.
		if runtime.GOOS == "linux" && runtime.GOARCH == "arm64" {
			syncSystemClock()
		}

		if err := engine.Setup(r.Context(), cfg); err != nil {
			logger.FromRequest(r).
				WithField("latency", time.Since(st)).
				WithField("time", time.Now().Format(time.RFC3339)).
				WithField("error", err).
				WithField("cfg", cfg).
				Infoln("api: failed stage setup")
			WriteError(w, err)
			return
		}

		if s.EgressPolicy != nil && s.EgressPolicy.ProxyURL != "" {
			logger.FromRequest(r).WithField("proxy_url", s.EgressPolicy.ProxyURL).Infoln("api: applying egress proxy config")
			if err := applyDockerProxy(s.EgressPolicy); err != nil {
				logger.FromRequest(r).WithError(err).Warnln("api: failed to configure docker proxy for egress")
			}
		} else {
			logger.FromRequest(r).Infoln("api: no egress proxy policy, skipping docker proxy config")
		}

		WriteJSON(w, api.SetupResponse{}, http.StatusOK)
		logger.FromRequest(r).
			WithField("latency", time.Since(st)).
			WithField("time", time.Now().Format(time.RFC3339)).
			Infoln("api: successfully completed the stage setup")
	}
}

func getSharedVolume() *spec.Volume {
	return &spec.Volume{
		HostPath: &spec.VolumeHostPath{
			Name: pipeline.SharedVolName,
			Path: pipeline.GetSharedVolPath(),
			ID:   "engine",
		},
	}
}

func getDockerSockVolume() *spec.Volume {
	path := engine.DockerSockUnixPath
	if runtime.GOOS == "windows" {
		path = engine.DockerSockWinPath
	}
	return &spec.Volume{
		HostPath: &spec.VolumeHostPath{
			Name: engine.DockerSockVolName,
			Path: path,
			ID:   "docker",
		},
	}
}

func setProxyEnvs(environment map[string]string) {
	proxyEnvs := []string{"http_proxy", "https_proxy", "no_proxy", "HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY"}
	for _, v := range proxyEnvs {
		if val, ok := environment[v]; ok {
			os.Setenv(v, val)
		}
	}
}

func getTiCfg(t *api.TIConfig, mtlsConfig *spec.MtlsConfig, envs map[string]string) tiCfg.Cfg {
	// Extract parent unique ID from environment variables
	parentUniqueID := ""
	if envs != nil {
		parentUniqueID = envs["HARNESS_PARENT_UNIQUE_ID"]
	}
	cfg := tiCfg.New(t.URL, t.Token, t.AccountID, t.OrgID, t.ProjectID, t.PipelineID, t.BuildID, t.StageID, t.Repo,
		t.Sha, t.CommitLink, t.SourceBranch, t.TargetBranch, t.CommitBranch, pipeline.GetSharedVolPath(), parentUniqueID, false, mtlsConfig.ClientCert, mtlsConfig.ClientCertKey)
	return cfg
}

// initializeLELogStreaming sets up log streaming for lite-engine logs using the provided LELogKey.
// This captures all stdout logs from the lite-engine application and streams them to the log service.
func initializeLELogStreaming(setupReq *api.SetupRequest, state *pipeline.State) error {
	// Only initialize if LELogKey is provided
	if setupReq.LELogKey == "" {
		return nil
	}

	// Get or create the log stream client
	logClient := state.GetLogStreamClient()

	// Create a live log writer for streaming lite-engine logs
	ctx := context.Background()
	logWriter := livelog.New(
		ctx,
		logClient,
		setupReq.LELogKey,
		"lite-engine",
		[]logstream.Nudge{},
		false, // don't print to stdout (would cause infinite loop)
		setupReq.LogConfig.TrimNewLineSuffix,
		false,
		false,
	)

	// Open the log stream
	if err := logWriter.Open(); err != nil {
		return fmt.Errorf("failed to open lite-engine log stream: %w", err)
	}

	// Store the writer in state for later cleanup
	state.SetLELogWriter(logWriter, setupReq.LELogKey)

	// Add a logrus hook to redirect logs to the stream writer
	// logrus.AddHook(logger.NewStreamHook(logWriter))

	logger.L.
		WithField("le_log_key", setupReq.LELogKey).
		Infoln("api: successfully initialized lite-engine log streaming")

	return nil
}

// initializeOSStatsStreaming sets up live log streaming for OS stats using the provided MemoryMetricsLogKey.
// This collects OS stats once per second and streams them to the log service (similar to engine:main).
func initializeOSStatsStreaming(setupReq *api.SetupRequest, state *pipeline.State) error {
	// MemoryMetricsLogKey is the log key to stream this under.
	if setupReq.MemoryMetricsLogKey == "" {
		return nil
	}

	// Get or create the log stream client
	logClient := state.GetLogStreamClient()

	// Create a live log writer for streaming OS stats
	ctx := context.Background()
	logWriter := livelog.New(
		ctx,
		logClient,
		setupReq.MemoryMetricsLogKey,
		"os-stats",
		[]logstream.Nudge{},
		false, // don't print to stdout
		setupReq.LogConfig.TrimNewLineSuffix,
		false, // don't skip opening stream
		false, // don't skip closing stream
	)

	// Open the log stream
	if err := logWriter.Open(); err != nil {
		return fmt.Errorf("failed to open os stats log stream: %w", err)
	}

	// Start the OS stats collection goroutine that writes to the livelog writer
	cancel, getSummaryData := osstats.StartOSStatsStreaming(ctx, logWriter, logger.L)

	// Store the writer, cancel, and getSummaryData in state for later cleanup (keyed by metrics key)
	state.SetOSStatsEntry(setupReq.MemoryMetricsLogKey, &pipeline.OSStatsEntry{
		Writer:         logWriter,
		Cancel:         cancel,
		GetSummaryData: getSummaryData,
	})

	logger.L.WithField("memory_metrics_log_key", setupReq.MemoryMetricsLogKey).
		Infoln("api: initialized os stats live streaming")

	return nil
}

func setHarnessEnvs(environment map[string]string) {
	harnessEnvs := []string{"HARNESS_EXECUTION_ID", "HARNESS_DELEGATE_TASK_ID"}
	for _, v := range harnessEnvs {
		if val, ok := environment[v]; ok && val != "" {
			os.Setenv(v, val)
		}
	}
}

// initializeDualLogHook adds a logrus hook that emits each lite-engine internal log entry
// as flat JSON to stdout for OTel collection, when dual logging is enabled.
func initializeDualLogHook(setupReq *api.SetupRequest) {
	ti := &setupReq.TIConfig
	taskID := ""
	if setupReq.Envs != nil {
		taskID = setupReq.Envs["HARNESS_DELEGATE_TASK_ID"]
	}

	planExecID := os.Getenv("HARNESS_EXECUTION_ID")

	meta := duallog.NewMetaConfig(
		ti.AccountID, ti.OrgID, ti.ProjectID, ti.PipelineID,
		ti.BuildID, planExecID, ti.StageID, "lite-engine", taskID,
	)
	logrus.AddHook(logger.NewDualLogHook(meta, "EXECUTION_LOGS"))

	logger.L.WithFields(logrus.Fields{
		"accountId": ti.AccountID, "pipelineId": ti.PipelineID,
		"stageId": ti.StageID, "taskId": taskID,
	}).Infoln("api: initialized dual log hook for lite-engine internal logs")
}

const (
	dockerDropInDirPerm  = 0o755
	dockerDropInFilePerm = 0o600
	dockerProxyTimeout   = 2 * time.Minute
)

// applyDockerProxy configures the docker daemon to route all outbound HTTP/S through the
// egress proxy. Credentials are URL-encoded via net/url so special chars are handled safely.
// docker is restarted so the daemon picks up the new settings.
//
// Linux: writes a systemd drop-in for docker.service and runs daemon-reload + docker restart.
// Windows: sets machine-level env vars via individual setx calls (no script interpolation).
func applyDockerProxy(policy *api.EgressPolicy) error {
	// Log the raw proxy URL only — never the credentialed form.
	logrus.WithField("proxy_url", policy.ProxyURL).Infoln("setup: applying docker proxy for egress")

	credURL, err := buildCredentialedProxyURL(policy.Username, policy.Password, policy.ProxyURL)
	if err != nil {
		return fmt.Errorf("failed to build proxy URL: %w", err)
	}

	switch runtime.GOOS {
	case "linux":
		return applyDockerProxyLinux(credURL, policy.NoProxy)
	case OSWindows:
		return applyDockerProxyWindows(credURL, policy.NoProxy)
	default:
		return nil
	}
}

// buildCredentialedProxyURL embeds username:password into the proxy URL using net/url so that
// special characters (@ : % $ etc.) in credentials are percent-encoded correctly.
// Bare URLs without a scheme are treated as http://.
func buildCredentialedProxyURL(username, password, proxyURL string) (string, error) {
	if username == "" || password == "" {
		return proxyURL, nil
	}
	raw := proxyURL
	if !strings.Contains(raw, "://") {
		raw = "http://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("invalid proxy URL %q: %w", proxyURL, err)
	}
	u.User = url.UserPassword(username, password)
	return u.String(), nil
}

func applyDockerProxyLinux(proxyURL, noProxy string) error {
	dropInDir := "/etc/systemd/system/docker.service.d"
	if err := os.MkdirAll(dropInDir, dockerDropInDirPerm); err != nil {
		return fmt.Errorf("failed to create docker drop-in dir: %w", err)
	}

	dropIn := fmt.Sprintf("[Service]\nEnvironment=\"HTTP_PROXY=%s\"\nEnvironment=\"HTTPS_PROXY=%s\"\nEnvironment=\"NO_PROXY=%s\"\n",
		proxyURL, proxyURL, noProxy)
	if err := os.WriteFile(filepath.Join(dropInDir, "http-proxy.conf"), []byte(dropIn), dockerDropInFilePerm); err != nil {
		return fmt.Errorf("failed to write docker proxy drop-in: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), dockerProxyTimeout)
	defer cancel()

	if out, err := exec.CommandContext(ctx, "systemctl", "daemon-reload").CombinedOutput(); err != nil {
		return fmt.Errorf("daemon-reload failed: %w — %s", err, string(out))
	}

	if out, err := exec.CommandContext(ctx, "systemctl", "restart", "docker").CombinedOutput(); err != nil {
		logrus.WithError(err).WithField("output", string(out)).Warnln("setup: docker restart after proxy config failed")
	}

	logrus.Infoln("setup: docker proxy configured for egress (linux)")
	return nil
}

func applyDockerProxyWindows(proxyURL, noProxy string) error {
	// Use individual argv-based calls — no string interpolation into a script, so
	// special characters in proxyURL/noProxy cannot break out of the argument.
	vars := [][2]string{
		{"HTTPS_PROXY", proxyURL},
		{"HTTP_PROXY", proxyURL},
		{"NO_PROXY", noProxy},
	}
	for _, kv := range vars {
		out, err := exec.Command("powershell", "-NonInteractive", "-Command",
			"[Environment]::SetEnvironmentVariable", kv[0], kv[1], "Machine").CombinedOutput()
		if err != nil {
			return fmt.Errorf("failed to set %s: %w — %s", kv[0], err, string(out))
		}
	}

	wctx, wcancel := context.WithTimeout(context.Background(), dockerProxyTimeout)
	defer wcancel()
	out, err := exec.CommandContext(wctx, "powershell", "-NonInteractive", "-Command",
		"try { Restart-Service docker -Force -ErrorAction Stop; Write-Host 'docker restarted' } catch { Write-Host 'docker not running yet' }").CombinedOutput()
	if err != nil {
		logrus.WithError(err).WithField("output", string(out)).Warnln("setup: docker restart after proxy config failed (windows)")
	}

	logrus.Infoln("setup: docker proxy configured for egress (windows)")
	return nil
}

// syncSystemClock forces chrony to step the system clock if there is significant drift.
// This fixes clock skew on ARM64 VMs after GCP hibernate resume, where the arch_sys_counter
// clock source doesn't auto-adjust (unlike x86's kvm-clock). Without this, chrony may
// mark the NTP source as too variable and refuse to step, leaving the clock minutes behind.
func syncSystemClock() {
	if _, err := exec.LookPath("chronyc"); err != nil {
		return
	}

	// Restart chrony to reset source state (clears the "too variable" flag from post-resume measurements)
	if out, err := exec.CommandContext(context.Background(), "systemctl", "restart", "chrony").CombinedOutput(); err != nil {
		logrus.WithError(err).WithField("output", string(out)).Warnln("setup: failed to restart chrony")
		return
	}

	// Force an immediate clock step
	if out, err := exec.CommandContext(context.Background(), "chronyc", "-a", "makestep").CombinedOutput(); err != nil {
		logrus.WithError(err).WithField("output", string(out)).Warnln("setup: failed to force chrony clock step")
		return
	}

	logrus.Infoln("setup: forced chrony clock sync on ARM64")
}
