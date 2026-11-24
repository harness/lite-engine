// Copyright 2026 Harness Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package telemetry

import (
	"context"
	"fmt"
	"time"

	"github.com/sirupsen/logrus"
	"go.opentelemetry.io/otel/attribute"
	otellog "go.opentelemetry.io/otel/log"
)

// diagnosticField marks a logrus entry as telemetry's own diagnostic message.
// The log hook never ships such entries: an export failure that is itself
// exported would loop forever.
const diagnosticField = "telemetry_internal"

// diagnostics returns the logrus entry telemetry must use for its own messages.
func diagnostics(logger *logrus.Logger) *logrus.Entry {
	return logger.WithField(diagnosticField, true)
}

// logHook is a logrus hook that forwards entries to an OTel logger.
//
// Fire is on the hot path of every log line in lite-engine, so it never
// blocks (the SDK's batch processor drops on overflow), never returns an error
// (logrus would print it) and never panics.
type logHook struct {
	logger otellog.Logger
	vmID   string
	levels []logrus.Level
}

var _ logrus.Hook = (*logHook)(nil)

// newLogHook ships entries at minLevel or more severe. Every record carries
// vmID: identity is welcome on logs (it is a record attribute, not a stream
// label, so it costs no cardinality).
func newLogHook(logger otellog.Logger, vmID string, minLevel logrus.Level) *logHook {
	return &logHook{logger: logger, vmID: vmID, levels: logrus.AllLevels[:minLevel+1]}
}

// Levels implements logrus.Hook.
func (h *logHook) Levels() []logrus.Level { return h.levels }

// Fire implements logrus.Hook.
func (h *logHook) Fire(entry *logrus.Entry) error {
	contain(func() { h.emit(entry) })
	return nil
}

func (h *logHook) emit(entry *logrus.Entry) {
	if _, internal := entry.Data[diagnosticField]; internal {
		return
	}
	var rec otellog.Record
	rec.SetTimestamp(entry.Time)
	rec.SetObservedTimestamp(time.Now())
	rec.SetSeverity(severityOf(entry.Level))
	rec.SetSeverityText(entry.Level.String())
	rec.SetBody(attribute.StringValue(entry.Message))
	rec.AddAttributes(h.attributes(entry)...)
	h.logger.Emit(context.Background(), rec)
}

func (h *logHook) attributes(entry *logrus.Entry) []attribute.KeyValue {
	attrs := make([]attribute.KeyValue, 0, len(entry.Data)+1)
	for key, value := range entry.Data {
		if key == attrVMID {
			continue // identity is ours to assert, not the log line's
		}
		attrs = append(attrs, attribute.String(key, fmt.Sprint(value)))
	}
	return append(attrs, attribute.String(attrVMID, h.vmID))
}

func severityOf(level logrus.Level) otellog.Severity {
	switch level {
	case logrus.PanicLevel, logrus.FatalLevel:
		return otellog.SeverityFatal
	case logrus.ErrorLevel:
		return otellog.SeverityError
	case logrus.WarnLevel:
		return otellog.SeverityWarn
	case logrus.InfoLevel:
		return otellog.SeverityInfo
	case logrus.DebugLevel:
		return otellog.SeverityDebug
	case logrus.TraceLevel:
		return otellog.SeverityTrace
	default:
		return otellog.SeverityInfo
	}
}
