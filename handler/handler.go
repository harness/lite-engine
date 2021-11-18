package handler

import (
	"io"
	"net/http"

	"github.com/harness/lite-engine/config"
	"github.com/harness/lite-engine/engine"
	"github.com/harness/lite-engine/logger"
	"github.com/harness/lite-engine/pipeline/runtime"

	"github.com/go-chi/chi"
)

// Handler returns an http.Handler that exposes the service resources.
func Handler(config *config.Config, engine *engine.Engine, stepExecutor *runtime.StepExecutor) http.Handler {
	r := chi.NewRouter()
	r.Use(logger.Middleware)

	// Setup stage endpoint
	r.Mount("/setup", func() http.Handler {
		sr := chi.NewRouter()
		sr.Post("/", HandleSetup(engine))
		return sr
	}())

	// Destroy stage endpoint
	r.Mount("/destroy", func() http.Handler {
		sr := chi.NewRouter()
		sr.Post("/", HandleDestroy(engine))
		return sr
	}())

	// Start step endpoint
	r.Mount("/start_step", func() http.Handler {
		sr := chi.NewRouter()
		sr.Post("/", HandleStartStep(stepExecutor))
		return sr
	}())

	// Poll step endpoint
	r.Mount("/poll_step", func() http.Handler {
		sr := chi.NewRouter()
		sr.Post("/", HandlePollStep(stepExecutor))
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
