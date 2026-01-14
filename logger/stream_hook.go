// Copyright 2022 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package logger

import (
	"fmt"

	"github.com/harness/lite-engine/logstream"
	"github.com/sirupsen/logrus"
)

// StreamHook is a logrus hook that writes log entries to a logstream.Writer.
// This allows streaming of lite-engine logs to a remote log service.
type StreamHook struct {
	writer logstream.Writer
}

// NewStreamHook creates a new StreamHook that writes to the given writer.
func NewStreamHook(writer logstream.Writer) *StreamHook {
	return &StreamHook{
		writer: writer,
	}
}

// Levels returns the log levels that this hook should be fired for.
// We capture all log levels.
func (h *StreamHook) Levels() []logrus.Level {
	return logrus.AllLevels
}

// Fire is called when a log event is fired.
func (h *StreamHook) Fire(entry *logrus.Entry) error {
	// Format the log entry similar to how logrus would format it
	msg := formatLogEntry(entry)

	// Write to the stream writer
	_, err := h.writer.Write([]byte(msg + "\n"))
	return err
}

// formatLogEntry formats a logrus entry into a readable log line.
func formatLogEntry(entry *logrus.Entry) string {
	// Format: time="2024-12-18T10:30:00Z" level=info msg="message" field1=value1 field2=value2
	timestamp := entry.Time.Format("2006-01-02T15:04:05Z07:00")
	level := entry.Level.String()
	msg := entry.Message

	// Start with time, level, and message
	logLine := fmt.Sprintf("time=%q level=%s msg=%q", timestamp, level, msg)

	// Add any additional fields
	for k, v := range entry.Data {
		logLine += fmt.Sprintf(" %s=%v", k, v)
	}

	return logLine
}
