// Copyright 2022 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"runtime"
	"time"

	"github.com/harness/lite-engine/api"
	"github.com/harness/lite-engine/engine"
	"github.com/harness/lite-engine/engine/spec"
	"github.com/harness/lite-engine/executor"
	"github.com/harness/lite-engine/logger"
	"github.com/harness/lite-engine/pipeline"
	leruntime "github.com/harness/lite-engine/pipeline/runtime"
)

// HandleExecuteStep returns an http.HandlerFunc that executes a step
func HandleSetup(engine *engine.Engine) http.HandlerFunc {
	fmt.Println("enter HandleSetup")
	return func(w http.ResponseWriter, r *http.Request) {
		st := time.Now()

		var s api.SetupRequest
		err := json.NewDecoder(r.Body).Decode(&s)
		if err != nil {
			WriteBadRequest(w, err)
			return
		}
		id := s.ID
		stepNumber := s.StepNumber
		config := s.SetupRequestConfig
		fmt.Println("Handle SetupRequest: %s", s)

		setProxyEnvs(config.Envs)
		state := pipeline.GetState()
		state.Set(config.Secrets, config.LogConfig, config.TIConfig)

		if config.MountDockerSocket == nil || *config.MountDockerSocket { // required to support m1 where docker isn't installed.
			config.Volumes = append(config.Volumes, getDockerSockVolume())
		}
		config.Volumes = append(config.Volumes, getSharedVolume())
		cfg := &spec.PipelineConfig{
			Envs:    config.Envs,
			Network: config.Network,
			Platform: spec.Platform{
				OS:   runtime.GOOS,
				Arch: runtime.GOARCH,
			},
			Volumes:           config.Volumes,
			Files:             config.Files,
			EnableDockerSetup: config.MountDockerSocket,
		}

		if err := engine.Setup(r.Context(), cfg); err != nil {
			logger.FromRequest(r).
				WithField("latency", time.Since(st)).
				WithField("time", time.Now().Format(time.RFC3339)).
				Infoln("api: failed stage setup")
			WriteError(w, err)
			return
		}

		stepExecutors := []*leruntime.StepExecutor{}
		for i := 0; i < stepNumber; i++ {
			stepExecutors = append(stepExecutors, leruntime.NewStepExecutor(engine))
		}
		// Add the state of this execution to the executor
		stageData := &executor.StageData{
			Engine:        engine,
			StepExecutors: stepExecutors,
			State:         state,
		}
		ex := executor.GetExecutor()
		ex.Add(id, stageData)

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
			Path: pipeline.SharedVolPath,
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
