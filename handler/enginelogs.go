// Copyright 2022 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package handler

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/harness/lite-engine/api"
	"github.com/harness/lite-engine/logger"
	"github.com/harness/lite-engine/oshelp"
	"github.com/sirupsen/logrus"
)

// HandleStreamEngineLogs handles the streaming of lite-engine's own log file
func HandleStreamEngineLogs() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		st := time.Now()

		var req api.StreamEngineLogsRequest
		err := json.NewDecoder(r.Body).Decode(&req)
		if err != nil {
			WriteBadRequest(w, err)
			return
		}

		// Determine log path based on OS
		logPath := os.Getenv("LITE_ENGINE_LOG_PATH")
		if logPath == "" {
			logPath = oshelp.GetLiteEngineLogsPath(oshelp.GetOS())
		}

		// Open the log file
		file, err := os.Open(logPath)
		if err != nil {
			// If file doesn't exist yet, return empty content
			if os.IsNotExist(err) {
				response := api.StreamEngineLogsResponse{
					Content: []byte{},
					Offset:  req.Offset,
				}
				WriteJSON(w, response, http.StatusOK)
				return
			}
			WriteError(w, err)
			return
		}
		defer file.Close()

		// Seek to the requested offset
		_, err = file.Seek(req.Offset, io.SeekStart)
		if err != nil {
			WriteError(w, err)
			return
		}

		// Read remaining content
		content, err := io.ReadAll(file)
		if err != nil {
			WriteError(w, err)
			return
		}

		newOffset := req.Offset + int64(len(content))

		response := api.StreamEngineLogsResponse{
			Content: content,
			Offset:  newOffset,
		}

		WriteJSON(w, response, http.StatusOK)

		logger.FromRequest(r).
			WithField("latency", time.Since(st)).
			WithField("time", time.Now().Format(time.RFC3339)).
			WithField("log_path", logPath).
			WithField("offset", req.Offset).
			WithField("new_offset", newOffset).
			WithField("bytes_read", len(content)).
			Traceln("api: successfully streamed engine logs")
	}
}

