// Copyright 2022 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package handler

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

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
		status := http.StatusOK

		performDNSLookup := performDNSLookup(r.URL.Query())
		if performDNSLookup {
			err := checkInternetConnectivity()
			if err != nil {
				WriteError(w, err)
				return
			}
		}

		WriteJSON(w, response, status)
	}
}

func checkInternetConnectivity() error {
	dialer := net.Dialer{
		Timeout: 2 * time.Second,
	}
	conn, err := dialer.Dial("tcp", "8.8.8.8:53")
	if err != nil {
		return fmt.Errorf("error connecting to 8.8.8.8:53 %w", err)
	}
	defer conn.Close()
	return nil
}

func performDNSLookup(values url.Values) bool {
	performDNSLookup := values.Get("perform_dns_lookup")
	return strings.EqualFold(performDNSLookup, "true")
}
