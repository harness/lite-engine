// Copyright 2022 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package handler

import (
	"net/http"

	"github.com/harness/lite-engine/config"
	"github.com/harness/lite-engine/engine"
	"github.com/harness/lite-engine/logger"
	"github.com/harness/lite-engine/pipeline/runtime"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

// Handler returns an http.Handler that exposes the service resources.
func Handler(config *config.Config, engine *engine.Engine, stepExecutor *runtime.StepExecutor) http.Handler {
	r := chi.NewRouter()
	r.Use(logger.Middleware)
	r.Use(middleware.Recoverer)

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

	// Get step log output
	r.Mount("/stream_output", func() http.Handler {
		sr := chi.NewRouter()
		sr.Post("/", HandleStreamOutput(stepExecutor))
		return sr
	}())

	// Health check
	r.Mount("/healthz", func() http.Handler {
		sr := chi.NewRouter()
		sr.Get("/", HandleHealth())
		return sr
	}())

	r.Mount("/suspend", func() http.Handler {
		sr := chi.NewRouter()
		sr.Post("/", HandleSuspend(engine))
		return sr
	}())

	// Workload Identity mint endpoint (OIDC-without-connector broker). hcli inside the step POSTs its
	// handle + identity name here to obtain a short-lived OIDC token; the workload token stays in
	// lite-engine.
	r.Mount("/mint_workload_token", func() http.Handler {
		sr := chi.NewRouter()
		sr.Post("/", HandleMintWorkloadToken())
		return sr
	}())

	return r
}

// MintHandler exposes ONLY the workload-identity mint endpoint. It is served on a separate plain-HTTP
// listener because the main server enforces mTLS (client cert), which the in-step hcli cannot present.
// The opaque per-step handle is the capability that authorizes the mint, and only short-lived OIDC
// tokens are returned, so mTLS is not required here.
func MintHandler() http.Handler {
	r := chi.NewRouter()
	r.Use(logger.Middleware)
	r.Use(middleware.Recoverer)
	r.Post("/mint_workload_token", HandleMintWorkloadToken())
	return r
}
