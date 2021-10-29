package handler

import (
	"io"
	"net/http"

	"github.com/drone/lite-engine/config"
	"github.com/drone/lite-engine/logger"

	"github.com/go-chi/chi"
)

// Handler returns an http.Handler that exposes the service resources.
func Handler(conf *config.Config) http.Handler {
	r := chi.NewRouter()
	r.Use(logger.Middleware)

	// Execute step endpoint
	// Format: /execute_step
	r.Mount("/execute_step", func() http.Handler {
		sr := chi.NewRouter()

		sr.Post("/", HandleExecuteStep())

		return sr
	}())

	// Liveness check
	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		_, writeErr := io.WriteString(w, "OK")
		if writeErr != nil {
			logger.FromRequest(r).
				WithError(writeErr).
				Errorln("cannot write healthz response")
		}
	})

	return r
}
