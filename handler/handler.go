package handler

import (
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

	// Health check
	r.Mount("/healthz", func() http.Handler {
		sr := chi.NewRouter()
		sr.Get("/", HandleHealth())
		return sr
	}())

	return r
}
