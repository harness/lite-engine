package handler

import (
	"context"
	"encoding/json"
	"github.com/harness/lite-engine/api"
	"github.com/harness/lite-engine/engine"
	"github.com/harness/lite-engine/logger"
	"github.com/harness/lite-engine/pipeline"
	"net/http"
	"time"
)

// HandleSuspend returns a http.HandlerFunc that suspends a VM
func HandleSuspend(engine *engine.Engine) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		startTime := time.Now()
		state := pipeline.GetState()
		var logErr error
		var logs string

		var suspendRequest api.SuspendRequest
		err := json.NewDecoder(request.Body).Decode(&suspendRequest)
		if err != nil {
			WriteBadRequest(response, err)
			return
		}

		if err := engine.Suspend(request.Context(), suspendRequest.Labels); err != nil {
			logger.FromRequest(request).
				WithField("latency", time.Since(startTime)).
				WithField("time", time.Now().Format(time.RFC3339)).
				WithField("error", err).
				Infoln("api: failed suspend")
			WriteError(response, err)
			return
		}

		// upload engine logs
		if suspendRequest.LogKey != "" && suspendRequest.LiteEnginePath != "" {
			client := state.GetLogStreamClient()
			logs, logErr = GetLiteEngineLog(suspendRequest.LiteEnginePath)
			if logErr != nil {
				logger.FromRequest(request).WithField("time", time.Now().
					Format(time.RFC3339)).WithError(err).Errorln("could not fetch lite engine logs")
			} else {
				// error out if logs don't upload in a minute so that the VM can be destroyed
				ctx, cancel := context.WithTimeout(request.Context(), 1*time.Minute)
				defer cancel()
				logErr = client.Upload(ctx, suspendRequest.LogKey, convert(logs))
				if logErr != nil {
					logger.FromRequest(request).WithField("time", time.Now().
						Format(time.RFC3339)).WithError(err).Errorln("could not upload lite engine logs")
				}
			}
		}

		WriteJSON(response, api.SuspendResponse{}, http.StatusOK)
		logger.FromRequest(request).
			WithField("latency", time.Since(startTime)).
			WithField("time", time.Now().Format(time.RFC3339)).
			Infoln("api: successfully completed the suspend")
	}
}
