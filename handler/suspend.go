package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/harness/lite-engine/api"
	"github.com/harness/lite-engine/engine"
	"github.com/harness/lite-engine/logger"
	"github.com/harness/lite-engine/pc"
)

// HandleSuspend returns a http.HandlerFunc that suspends a VM.
// Private Connectivity must fully remove stage resources and then logout before hibernate so
// warm reuse cannot retain Docker or tailnet state after /run markers vanish.
func HandleSuspend(engine *engine.Engine) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		startTime := time.Now()

		var suspendRequest api.SuspendRequest
		err := json.NewDecoder(request.Body).Decode(&suspendRequest)
		if err != nil {
			WriteBadRequest(response, err)
			return
		}
		pcUsed := pc.WasUsed()
		pcStateUnavailable := pcUsed && !engine.PrivateConnectivityConfigured()
		var suspendErr error
		if pcStateUnavailable {
			suspendErr = fmt.Errorf(
				"private connectivity cleanup state is unavailable after lite-engine restart; discard this VM")
		} else {
			suspendErr = engine.Suspend(request.Context(), suspendRequest.Labels)
		}

		// For PC, Engine.Suspend performs full stage-resource cleanup first. Logout is the final
		// network boundary. A restarted LE still attempts logout but keeps the reuse fence.
		if pcUsed || pc.NeedsNetworkCleanup() {
			logger.FromRequest(request).
				WithField("pc_state_available", !pcStateUnavailable).
				Infoln("api: starting private connectivity logout before suspend")
			if logoutErr := pc.Logout(request.Context()); logoutErr != nil {
				logger.FromRequest(request).
					WithField("latency", time.Since(startTime)).
					WithField("time", time.Now().Format(time.RFC3339)).
					WithError(logoutErr).
					Errorln("api: private connectivity logout before suspend failed")
				suspendErr = errors.Join(suspendErr, fmt.Errorf("pc logout failed: %w", logoutErr))
			}
		}

		if suspendErr != nil {
			logger.FromRequest(request).
				WithField("latency", time.Since(startTime)).
				WithField("time", time.Now().Format(time.RFC3339)).
				WithField("error", suspendErr).
				Infoln("api: failed suspend")
			WriteError(response, suspendErr)
			return
		}
		if pcUsed {
			if cleanupErr := pc.MarkCleanupComplete(); cleanupErr != nil {
				WriteError(response, cleanupErr)
				return
			}
		}

		WriteJSON(response, api.SuspendResponse{}, http.StatusOK)
		logger.FromRequest(request).
			WithField("latency", time.Since(startTime)).
			WithField("time", time.Now().Format(time.RFC3339)).
			Infoln("api: successfully completed the suspend")
	}
}
