// Copyright 2022 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/harness/lite-engine/api"
	"github.com/harness/lite-engine/engine"
	"github.com/harness/lite-engine/engine/spec"
	"github.com/harness/lite-engine/logger"
	"github.com/harness/lite-engine/pipeline"
)

// HandleDestroy returns an http.HandlerFunc that destroy the stage resources
func HandleDestroy(engine *engine.Engine) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		st := time.Now()
		state := pipeline.GetState()
		log := logger.FromRequest(r)

		log.Infoln("api: destroy request received")

		var d api.DestroyRequest
		err := json.NewDecoder(r.Body).Decode(&d)
		if err != nil {
			WriteBadRequest(w, err)
			return
		}

		destroyErr := engine.Destroy(r.Context())

		// upload engine logs
		if d.LogKey != "" && d.LiteEnginePath != "" {
			if !d.LogDrone {
				client := state.GetLogStreamClient()
				logs, logErr = GetLiteEngineLog(d.LiteEnginePath)
				if logErr != nil {
					logger.FromRequest(r).WithField("time", time.Now().
						Format(time.RFC3339)).WithError(err).Errorln("could not fetch lite engine logs")
				} else {
					// error out if logs don't upload in a minute so that the VM can be destroyed
					ctx, cancel := context.WithTimeout(r.Context(), 1*time.Minute)
					defer cancel()
					logErr = client.Upload(ctx, d.LogKey, convert(logs))
					if logErr != nil {
						logger.FromRequest(r).WithField("time", time.Now().
							Format(time.RFC3339)).WithError(err).Errorln("could not upload lite engine logs")
					} else {
						// Close lite-engine log stream only if upload was successful
						if closeErr := closeLELogStream(state); closeErr != nil {
							logger.FromRequest(r).
								WithField("time", time.Now().Format(time.RFC3339)).
								WithError(closeErr).
								Warnln("api: failed to close lite-engine log stream")
						}
					}
				}
				if d.StageRuntimeID != "" {
					pipeline.GetEnvState().Delete(d.StageRuntimeID)
				}
			}
			// else {
			// TODO: handle drone case for lite engine log upload
			// }
		}

		stats := &spec.OSStats{}

		collector := state.GetStatsCollector()
		if collector != nil {
			collector.Stop()
			collector.Aggregate()
			stats = collector.Stats()
		}

		// Stop OS stats live streaming and close the writer (which flushes and uploads).
		if err := closeOSStatsStream(state); err != nil {
			logger.FromRequest(r).
				WithField("time", time.Now().Format(time.RFC3339)).
				WithError(err).
				Warnln("api: failed to close os stats stream")
		}

		if destroyErr != nil || logErr != nil {
			WriteError(w, fmt.Errorf("destroy error: %w, lite engine log error: %v", destroyErr, logErr))
			return
		}

		WriteJSON(w, api.DestroyResponse{OSStats: stats}, http.StatusOK)

		logger.FromRequest(r).
			WithField("latency", time.Since(st)).
			WithField("time", time.Now().Format(time.RFC3339)).
			Infoln("api: successfully destroyed the stage resources")
	}
}

// closeLELogStream closes the lite-engine log stream writer if it exists.
func closeLELogStream(_ context.Context, state *pipeline.State) error {
	writer := state.GetLELogWriter()
	if writer == nil {
		return nil
	}

	logKey := state.GetLELogKey()
	logger.L.
		WithField("le_log_key", logKey).
		Infoln("api: closing lite-engine log stream")

	// Close the writer to flush and upload remaining logs
	if err := writer.Close(); err != nil {
		return fmt.Errorf("failed to close lite-engine log writer: %w", err)
	}

	// Clear the writer from state
	state.SetLELogWriter(nil, "")
	return nil
}

// closeOSStatsStream stops the OS stats collection goroutine and closes the log stream writer.
func closeOSStatsStream(state *pipeline.State) error {
	// First, stop the stats collection goroutine
	if cancel := state.GetOSStatsCancel(); cancel != nil {
		cancel()
	}

	writer := state.GetOSStatsWriter()
	if writer == nil {
		return nil
	}

	logKey := state.GetOSStatsKey()
	logger.L.
		WithField("os_stats_key", logKey).
		Infoln("api: closing os stats log stream")

	// Close the writer to flush and upload remaining logs
	if err := writer.Close(); err != nil {
		return fmt.Errorf("failed to close os stats log writer: %w", err)
	}

	// Clear the writer from state
	state.SetOSStatsWriter(nil, "", nil)
	return nil
}
