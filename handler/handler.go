package handler

import (
	"encoding/json"
	"io"
	"net/http"
	"os"

	"github.com/drone-runners/drone-runner-docker/engine"
	"github.com/drone/lite-engine/config"
	"github.com/drone/lite-engine/logger"
	"github.com/sirupsen/logrus"

	"github.com/go-chi/chi"
)

func getSpec() *engine.Spec {
	return &engine.Spec{
		Platform: engine.Platform{},
		Steps:    []*engine.Step{},
		Internal: []*engine.Step{},
		Volumes: []*engine.Volume{
			{
				HostPath: &engine.VolumeHostPath{
					ID:   "drone",
					Name: "_workspace",
					Path: "/tmp/lite-engine",
				},
			},
		},
		Network: engine.Network{
			ID: "drone1234",
		},
	}
}

// Handler returns an http.Handler that exposes the
// service resources.
func Handler(config config.Config, docker *engine.Docker) http.Handler {
	r := chi.NewRouter()
	r.Use(logger.Middleware)

	r.Post("/setup", func(w http.ResponseWriter, r *http.Request) {
		spec := getSpec()
		logrus.Infof("setup : %s", spec)

		if err := docker.Setup(r.Context(), spec); err != nil {
			logrus.WithError(err).Errorln("failed to setup docker")
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	})

	r.Post("/run", func(w http.ResponseWriter, r *http.Request) {
		step := &engine.Step{}
		spec := getSpec()
		logrus.Infof("step : %s, %s", spec, step)

		if err := json.NewDecoder(r.Body).Decode(step); err != nil {
			logrus.WithError(err).Errorln("failed to read step request")
			http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
			return
		}

		state, err := docker.Run(r.Context(), spec, step, os.Stdout)
		if err != nil {
			logrus.WithError(err).Errorln("failed to setup docker")
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)

		_ = json.NewEncoder(w).Encode(state)
	})

	r.Post("/destroy", func(w http.ResponseWriter, r *http.Request) {
		spec := getSpec()
		logrus.Infof("destroy : %s", spec)

		if err := docker.Destroy(r.Context(), spec); err != nil {
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	})

	// Liveness check
	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "OK")
	})

	return r
}
