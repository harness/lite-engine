package handler

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/harness/lite-engine/api"
	"github.com/harness/lite-engine/engine"
	"github.com/harness/lite-engine/engine/spec"
	"github.com/harness/lite-engine/logger"
	"github.com/harness/lite-engine/pipeline"
)

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

		state := pipeline.GetState()
		state.Set(s.Secrets, s.LogConfig, s.TIConfig)

		s.Volumes = append(s.Volumes, getSharedVolume())
		cfg := &spec.PipelineConfig{
			Envs:     s.Envs,
			Network:  s.Network,
			Platform: s.Platform,
			Volumes:  s.Volumes,
		}
		if err := engine.Setup(r.Context(), cfg); err != nil {
			logger.FromRequest(r).
				WithField("latency", time.Since(st)).
				WithField("time", time.Now().Format(time.RFC3339)).
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
			Path: pipeline.SharedVolPath,
			ID:   "engine",
		},
	}
}
