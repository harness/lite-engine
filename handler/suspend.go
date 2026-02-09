package handler

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/harness/lite-engine/api"
	"github.com/harness/lite-engine/engine"
	"github.com/harness/lite-engine/logger"
)

// HandleSuspend returns a http.HandlerFunc that suspends a VM
func HandleSuspend(engine *engine.Engine) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		startTime := time.Now()

		var suspendRequest api.SuspendRequest
		err := json.NewDecoder(request.Body).Decode(&suspendRequest)
		if err != nil {
			WriteBadRequest(response, err)
			return
		}

		if suspendErr := engine.Suspend(request.Context(), suspendRequest.Labels); suspendErr != nil {
			logger.FromRequest(request).
				WithField("latency", time.Since(startTime)).
				WithField("time", time.Now().Format(time.RFC3339)).
				WithField("error", suspendErr).
				Infoln("api: failed suspend")
			WriteError(response, suspendErr)
			return
		}

		WriteJSON(response, api.SuspendResponse{}, http.StatusOK)
		logger.FromRequest(request).
			WithField("latency", time.Since(startTime)).
			WithField("time", time.Now().Format(time.RFC3339)).
			Infoln("api: successfully completed the suspend")
	}
}
