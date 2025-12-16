// Copyright 2022 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package handler

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/harness/lite-engine/api"
	"github.com/harness/lite-engine/version"
	"github.com/sirupsen/logrus"
)

const (
	defaultConnectivityCheckDuration = 5 * time.Second
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
			checkDuration := getConnectivityCheckDuration(r.URL.Query())
			err := checkInternetConnectivity(checkDuration)
			if err != nil {
				WriteError(w, err)
				return
			}
		}

		WriteJSON(w, response, status)
	}
}

func checkInternetConnectivity(duration time.Duration) error {
	logrus.Infof("Checking internet connectivity to 8.8.8.8:53 for %v", duration)

	startTime := time.Now()
	checkInterval := 500 * time.Millisecond
	checkCount := 0

	for time.Since(startTime) < duration {
		checkCount++
		dialer := net.Dialer{
			Timeout: 2 * time.Second,
		}
		conn, err := dialer.Dial("tcp", "8.8.8.8:53")
		if err != nil {
			return fmt.Errorf("connectivity check failed after %d attempts (elapsed: %v): error connecting to 8.8.8.8:53 %w",
				checkCount, time.Since(startTime), err)
		}
		conn.Close()

		// If we haven't reached the duration yet, wait before next check
		if time.Since(startTime) < duration {
			time.Sleep(checkInterval)
		}
	}

	logrus.Infof("Internet connectivity verified: %d successful checks over %v", checkCount, duration)
	return nil
}

func performDNSLookup(values url.Values) bool {
	performDNSLookup := values.Get("perform_dns_lookup")
	return strings.EqualFold(performDNSLookup, "true")
}

func getConnectivityCheckDuration(values url.Values) time.Duration {
	durationParam := values.Get("connectivity_check_duration")

	if durationParam == "" {
		return defaultConnectivityCheckDuration
	}

	// Try to parse as seconds (integer)
	if seconds, err := strconv.Atoi(durationParam); err == nil {
		if seconds <= 0 {
			logrus.Warnf("Invalid connectivity check duration %d seconds, using default %v", seconds, defaultConnectivityCheckDuration)
			return defaultConnectivityCheckDuration
		}
		return time.Duration(seconds) * time.Second
	}
	logrus.Warnf("Failed to parse connectivity check duration '%s', using default %v", durationParam, defaultConnectivityCheckDuration)
	return defaultConnectivityCheckDuration
}
