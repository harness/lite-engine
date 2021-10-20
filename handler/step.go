package handler

import (
	"net/http"
	"time"

	"github.com/drone/lite-engine/logger"
)

// HandleExecuteStep returns an http.HandlerFunc that executes a step
func HandleExecuteStep() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// ctx := r.Context()
		st := time.Now()

		logger.FromRequest(r).
			WithField("latency", time.Since(st)).
			WithField("time", time.Now().Format(time.RFC3339)).
			Infoln("api: successfully executed the step")
		w.WriteHeader(http.StatusNoContent)
	}
}
