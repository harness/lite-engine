// Copyright 2026 Harness Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package server

import (
	"net"
	"net/http"
	"os"
	"time"

	"github.com/harness/lite-engine/internal/safego"
	"github.com/harness/lite-engine/procmetrics"
	"github.com/sirupsen/logrus"
)

const defaultProcessMetricsAddr = "127.0.0.1:9099"

// startProcessMetricsServer serves /metrics and /debug/pprof/ when
// LITE_ENGINE_PROCESS_METRICS=true. The bind address must be loopback.
func startProcessMetricsServer() {
	addr, ok := processMetricsAddr(os.Getenv("LITE_ENGINE_PROCESS_METRICS"), os.Getenv("LITE_ENGINE_PROCESS_METRICS_ADDR"))
	if !ok {
		return
	}
	srv := &http.Server{
		Addr:              addr,
		Handler:           procmetrics.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	safego.SafeGo("process_metrics", func() {
		logrus.Infof("process metrics and pprof listening at http://%s", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logrus.WithError(err).Errorln("process metrics server stopped")
		}
	})
}

func processMetricsAddr(enabled, addr string) (string, bool) {
	if enabled != "true" && enabled != "1" {
		return "", false
	}
	if addr == "" {
		addr = defaultProcessMetricsAddr
	}
	host, _, err := net.SplitHostPort(addr)
	if err != nil || !isLoopbackHost(host) {
		logrus.Errorf("refusing process metrics bind %q; only loopback addresses are allowed", addr)
		return "", false
	}
	return addr, true
}

func isLoopbackHost(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
