// Copyright 2024 Harness Inc. All rights reserved.
// Use of this source code is governed by the PolyForm Free Trial 1.0.0 license
// that can be found in the licenses directory at the root of this repository, also available at
// https://polyformproject.org/wp-content/uploads/2020/05/PolyForm-Free-Trial-1.0.0.txt.

package logger

import (
	"strings"

	"github.com/harness/lite-engine/duallog"
	"github.com/sirupsen/logrus"
)

// DualLogHook is a logrus hook that emits each log entry as flat JSON to stdout
// via duallog.EmitLine for OTel collection. It uses fmt.Fprintln(os.Stdout, ...)
// internally (via EmitLine) so there is no recursion risk with logrus.
type DualLogHook struct {
	meta    *duallog.Meta
	logType string
}

// NewDualLogHook creates a DualLogHook that will emit JSON logs to stdout.
func NewDualLogHook(meta *duallog.Meta, logType string) *DualLogHook {
	return &DualLogHook{
		meta:    meta,
		logType: logType,
	}
}

// Levels returns all log levels so every logrus entry is captured.
func (h *DualLogHook) Levels() []logrus.Level {
	return logrus.AllLevels
}

// Fire is called for each logrus entry. It formats the entry and emits
// a flat JSON line via duallog.EmitLine.
func (h *DualLogHook) Fire(entry *logrus.Entry) error {
	msg := formatLogEntry(entry)
	level := strings.ToUpper(entry.Level.String())
	duallog.EmitLineWithLevel(h.meta, msg, entry.Time, h.logType, level)
	return nil
}
