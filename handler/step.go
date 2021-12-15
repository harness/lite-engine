package handler

import (
	"encoding/json"
	"net/http"
	"runtime"
	"time"

	"github.com/harness/lite-engine/api"
	"github.com/harness/lite-engine/engine/spec"
	"github.com/harness/lite-engine/logger"
	"github.com/harness/lite-engine/pipeline"
	pruntime "github.com/harness/lite-engine/pipeline/runtime"
)

// HandleExecuteStep returns an http.HandlerFunc that executes a step
func HandleStartStep(e *pruntime.StepExecutor) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		st := time.Now()

		var s api.StartStepRequest
		err := json.NewDecoder(r.Body).Decode(&s)
		if err != nil {
			WriteBadRequest(w, err)
			return
		}

		s.Volumes = append(s.Volumes, getSharedVolumeMount(), getDockerSockVolumeMount())

		if err := e.StartStep(r.Context(), &s); err != nil {
			WriteError(w, err)
		} else {
			WriteJSON(w, api.StartStepResponse{}, http.StatusOK)
		}

		logger.FromRequest(r).
			WithField("latency", time.Since(st)).
			WithField("time", time.Now().Format(time.RFC3339)).
			Infoln("api: successfully started the step")
	}
}

func HandlePollStep(e *pruntime.StepExecutor) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		st := time.Now()

		var s api.PollStepRequest
		err := json.NewDecoder(r.Body).Decode(&s)
		if err != nil {
			WriteBadRequest(w, err)
			return
		}

		if response, err := e.PollStep(r.Context(), &s); err != nil {
			WriteError(w, err)
		} else {
			WriteJSON(w, response, http.StatusOK)
		}

		logger.FromRequest(r).
			WithField("latency", time.Since(st)).
			WithField("time", time.Now().Format(time.RFC3339)).
			Infoln("api: successfully polled the step response")
	}
}

func getSharedVolumeMount() *spec.VolumeMount {
	return &spec.VolumeMount{
		Name: pipeline.SharedVolName,
		Path: pipeline.SharedVolPath,
	}
}

func getDockerSockVolumeMount() *spec.VolumeMount {
	path := pipeline.DockerSockUnixPath
	if runtime.GOOS == "windows" {
		path = pipeline.DockerSockWinPath
	}
	return &spec.VolumeMount{
		Name: pipeline.DockerSockVolName,
		Path: path,
	}
}
