// Copyright 2022 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/harness/lite-engine/api"
	"github.com/harness/lite-engine/engine"
	"github.com/harness/lite-engine/logger"
	"github.com/harness/lite-engine/logstream"
	"github.com/harness/lite-engine/pipeline"
)

func GetLiteEngineLog(osType string) (string, error) {
	switch osType {
	// TODO: Add for windows and mac
	case "linux":
		home, err := os.UserHomeDir()
		if err != nil {
			return "", errors.New("could not fetch home dir")
		}
		path := filepath.Join(home, "lite-engine.log")
		content, err := os.ReadFile(path)
		if err != nil {
			return "", err
		}
		return string(content), nil
	default:
		return "", errors.New("no log file")
	}
}

func convert(logs string) []*logstream.Line {
	lines := []*logstream.Line{}
	for idx, line := range strings.Split(logs, "\n") {
		lines = append(lines, &logstream.Line{Message: line, Number: idx})
	}
	return lines
}

// HandleDestroy returns an http.HandlerFunc that destroy the stage resources
func HandleDestroy(engine *engine.Engine) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		st := time.Now()

		var uploadErr error
		var logs string

		// Upload lite engine logs if key is set
		var d api.DestroyRequest
		err := json.NewDecoder(r.Body).Decode(&d)
		if err != nil {
			WriteBadRequest(w, err)
			return
		}

		if d.LogKey != "" {
			if !d.LogDrone {
				state := pipeline.GetState()
				client := state.GetLogStreamClient()
				logs, uploadErr = GetLiteEngineLog(runtime.GOOS)
				if uploadErr != nil {
					logger.FromRequest(r).WithField("time", time.Now().Format(time.RFC3339)).WithError(err).Errorln("could not fetch lite engine logs")
				} else {
					// error out if logs don't upload in a minute so that the VM can be destroyed
					ctx, cancel := context.WithTimeout(r.Context(), 1*time.Minute)
					defer cancel()
					uploadErr = client.Upload(ctx, d.LogKey, convert(logs))
					if uploadErr != nil {
						logger.FromRequest(r).WithField("time", time.Now().Format(time.RFC3339)).WithError(err).Errorln("could not upload lite engine logs")
					}
				}
			} else {
				// TODO: handle drone case for lite engine log upload
			}
		}

		destroyErr := engine.Destroy(r.Context())
		if destroyErr != nil || uploadErr != nil {
			WriteError(w, fmt.Errorf("destroy error: %w, upload error: %w", destroyErr, uploadErr))
		}

		WriteJSON(w, api.DestroyResponse{}, http.StatusOK)

		logger.FromRequest(r).
			WithField("latency", time.Since(st)).
			WithField("time", time.Now().Format(time.RFC3339)).
			Infoln("api: successfully destroyed the stage resources")
	}
}
