// Copyright 2026 Harness Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package telemetry

import (
	"context"
	"net/http"
	"net/url"
	"time"

	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
)

const (
	metricsPath = "v1/metrics"
	logsPath    = "v1/logs"

	retryInitialInterval = 500 * time.Millisecond
	retryMaxInterval     = 2 * time.Second
)

// signalURL returns the OTLP/HTTP URL for one signal under the base endpoint,
// preserving any path prefix of a fronting gateway.
func signalURL(base, signal string) string {
	u, err := url.Parse(base)
	if err != nil {
		return base // validate() has already vetted the endpoint
	}
	return u.JoinPath(signal).String()
}

// directConnection disables proxying. The OTLP gateway is a private address
// that hosted VMs must reach directly, while HTTP(S)_PROXY in lite-engine's
// environment points at the egress proxy used for customer traffic.
func directConnection(*http.Request) (*url.URL, error) { return nil, nil } //nolint:nilnil // nil URL means "no proxy"

func newOTLPMetricExporter(cfg Config) (sdkmetric.Exporter, error) {
	return otlpmetrichttp.New(context.Background(),
		otlpmetrichttp.WithEndpointURL(signalURL(cfg.Endpoint, metricsPath)),
		otlpmetrichttp.WithTemporalitySelector(temporalitySelector(cfg.MetricsVMID)),
		otlpmetrichttp.WithCompression(otlpmetrichttp.GzipCompression),
		otlpmetrichttp.WithProxy(directConnection),
		otlpmetrichttp.WithTimeout(cfg.ExportTimeout),
		otlpmetrichttp.WithRetry(otlpmetrichttp.RetryConfig{
			Enabled:         true,
			InitialInterval: retryInitialInterval,
			MaxInterval:     retryMaxInterval,
			MaxElapsedTime:  cfg.ExportTimeout,
		}),
	)
}

func newOTLPLogExporter(cfg Config) (sdklog.Exporter, error) {
	return otlploghttp.New(context.Background(),
		otlploghttp.WithEndpointURL(signalURL(cfg.Endpoint, logsPath)),
		otlploghttp.WithCompression(otlploghttp.GzipCompression),
		otlploghttp.WithProxy(directConnection),
		otlploghttp.WithTimeout(cfg.ExportTimeout),
		otlploghttp.WithRetry(otlploghttp.RetryConfig{
			Enabled:         true,
			InitialInterval: retryInitialInterval,
			MaxInterval:     retryMaxInterval,
			MaxElapsedTime:  cfg.ExportTimeout,
		}),
	)
}
