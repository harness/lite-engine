// Copyright 2022 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/harness/lite-engine/api"
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

const OSWindows = "windows"

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
func HandleSetup(engine *engine.Engine) http.HandlerFunc {
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
		state := pipeline.GetState()
		state.Set(s.Secrets, s.LogConfig, getTiCfg(&s.TIConfig, &s.MtlsConfig, s.Envs), s.MtlsConfig, collector)

		// Initialize lite-engine log streaming if LELogKey is provided
		if err := initializeLELogStreaming(&s, state); err != nil {
			logger.FromRequest(r).
				WithField("time", time.Now().Format(time.RFC3339)).
				WithError(err).
				Warnln("api: failed to initialize lite-engine log streaming")
		}

		// Initialize OS stats NDJSON streaming (file + upload) if MemoryMetrics key is provided
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
		}
		collector.Start()
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
		os.Setenv(v, environment[v])
	}
}

func getTiCfg(t *api.TIConfig, mtlsConfig *spec.MtlsConfig, envs map[string]string) tiCfg.Cfg {
	// Extract parent unique ID from environment variables
	parentUniqueID := ""
	if envs != nil {
		parentUniqueID = envs["HARNESS_PARENT_UNIQUE_ID"]
	}
	cfg := tiCfg.New(t.URL, t.Token, t.AccountID, t.OrgID, t.ProjectID, t.PipelineID, t.BuildID, t.StageID, t.Repo,
		t.Sha, t.CommitLink, t.SourceBranch, t.TargetBranch, t.CommitBranch, pipeline.GetSharedVolPath(), parentUniqueID, t.ParseSavings, false, mtlsConfig.ClientCert, mtlsConfig.ClientCertKey)
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
	logrus.AddHook(logger.NewStreamHook(logWriter))

	logger.L.
		WithField("le_log_key", setupReq.LELogKey).
		Infoln("api: successfully initialized lite-engine log streaming")

	return nil
}

// initializeOSStatsStreaming starts collecting OS stats once per second into an NDJSON file,
// and stores it in state for later upload in destroy.
func initializeOSStatsStreaming(setupReq *api.SetupRequest, state *pipeline.State) error {
	// memory_metrics is the log key to stream this file under.
	if setupReq.MemoryMetrics == "" {
		return nil
	}

	uploadKey := setupReq.MemoryMetrics
	localName := osstats.SanitizeFilename(uploadKey)
	localPath := filepath.Join(pipeline.GetSharedVolPath(), localName)

	if err := os.MkdirAll(pipeline.GetSharedVolPath(), 0o755); err != nil { //nolint:mnd
		return err
	}

	streamer, err := osstats.NewNDJSONStreamer(context.Background(), localPath, logger.L)
	if err != nil {
		return err
	}
	streamer.Start()
	state.SetOSStatsStreamer(streamer, uploadKey)

	logger.L.WithField("memory_metrics", setupReq.MemoryMetrics).
		WithField("memory_metrics_key", uploadKey).
		WithField("os_stats_path", localPath).
		Infoln("api: initialized os stats streaming")

	return nil
}
