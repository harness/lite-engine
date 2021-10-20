package handler

import (
	"io"
	"net/http"

	"github.com/drone/lite-engine/config"
	"github.com/drone/lite-engine/logger"

	"github.com/go-chi/chi"
)

// Handler returns an http.Handler that exposes the
// service resources.
func Handler(config config.Config) http.Handler {
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
		io.WriteString(w, "OK")
	})

	return r
}
