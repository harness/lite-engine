// Copyright 2026 Harness Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package telemetry

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"time"
)

const (
	defaultInterval      = 30 * time.Second
	defaultExportTimeout = 5 * time.Second
	unknownVMID          = "unknown"
)

// Metric dimension names. They are reserved: operator supplied labels may not
// reuse them, so a label can never shadow what the recorder itself reports.
const (
	attrSubsystem = "subsystem"
	attrOp        = "op"
	attrOutcome   = "outcome"
	attrVMID      = "vm_id"
	attrOS        = "os"
	attrArch      = "arch"
	attrVersion   = "version"
)

var (
	reservedLabels = map[string]struct{}{
		attrSubsystem: {}, attrOp: {}, attrOutcome: {}, attrVMID: {}, attrOS: {}, attrArch: {}, attrVersion: {},
	}
	labelNamePattern = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)
)

// Config controls telemetry. It is delivered in the setup request, never via
// the VM environment (which step code can read).
type Config struct {
	// Enabled switches telemetry on. When false nothing is started.
	Enabled bool
	// Endpoint is the OTLP/HTTP base URL, e.g. http://10.0.0.1:4318.
	Endpoint string
	// MetricsVMID adds a vm_id label to every metric and switches to
	// cumulative temporality with zero baselines. Off by default: it exists as
	// a fallback should the backend be unable to aggregate VM-less deltas.
	MetricsVMID bool
	// MetricLabels are fleet-level labels (pool, region, ...) added to every
	// metric. They must not identify an individual VM.
	MetricLabels map[string]string

	// Interval is both the metric export period and the process sampling period.
	Interval time.Duration
	// ExportTimeout bounds a single export attempt and any flush or shutdown.
	ExportTimeout time.Duration
}

// withDefaults returns a copy of c with unset tunables filled in.
func (c Config) withDefaults() Config {
	if c.Interval <= 0 {
		c.Interval = defaultInterval
	}
	if c.ExportTimeout <= 0 {
		c.ExportTimeout = defaultExportTimeout
	}
	return c
}

// validate reports whether an enabled config can be started. A disabled
// config is always valid.
func (c Config) validate() error {
	if !c.Enabled {
		return nil
	}
	if err := validateEndpoint(c.Endpoint); err != nil {
		return err
	}
	for name := range c.MetricLabels {
		if _, reserved := reservedLabels[name]; reserved {
			return fmt.Errorf("metric label %q is reserved", name)
		}
		if !labelNamePattern.MatchString(name) {
			return fmt.Errorf("invalid metric label name %q", name)
		}
	}
	return nil
}

func validateEndpoint(endpoint string) error {
	if endpoint == "" {
		return errors.New("telemetry endpoint is required")
	}
	u, err := url.Parse(endpoint)
	if err != nil {
		return fmt.Errorf("invalid telemetry endpoint: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("unsupported telemetry endpoint scheme %q", u.Scheme)
	}
	if u.Host == "" {
		return errors.New("telemetry endpoint has no host")
	}
	return nil
}

// resolveVMID names this VM for log records. It never fails: telemetry must
// not depend on the hostname being resolvable.
func resolveVMID(hostname func() (string, error)) string {
	if name, err := hostname(); err == nil && name != "" {
		return name
	}
	return unknownVMID
}

func osHostname() (string, error) { return os.Hostname() }
