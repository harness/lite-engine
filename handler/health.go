// Copyright 2022 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package handler

import (
	"net/http"

	"github.com/harness/lite-engine/api"
	"github.com/harness/lite-engine/version"
	"github.com/sirupsen/logrus"
)

func HandleHealth() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		logrus.Infoln("handler: HandleHealth()")
		version := version.Version
		response := api.HealthResponse{
			Version: version,
			OK:      true,
		}
		WriteJSON(w, response, http.StatusOK)
	}
}
