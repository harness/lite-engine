// Copyright 2025 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

// Package logutil provides utilities for custom error categorization log files.
package logutil

import (
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"

	"github.com/harness/lite-engine/pipeline"
	"github.com/sirupsen/logrus"
)

// Constants for custom error categorization log files
const (
	HarnessInternalLogsDir          = ".harness-internal/logs"
	StdoutLogSuffix                 = "-stdout.log"
	StderrLogSuffix                 = "-stderr.log"
	LogDirPermissions               = 0700
	CustomErrorCategorizationEnvVar = "CI_CUSTOM_ERROR_CATEGORIZATION"
	trueValue                       = "true"
	windowsOS                       = "windows"
)

// LogFileHandles holds the file handles and paths for custom error categorization log files.
type LogFileHandles struct {
	StdoutFile *os.File
	StderrFile *os.File
	StdoutPath string
	StderrPath string
}

// IsCustomErrorCategorizationEnabled checks if the custom error categorization feature is enabled
func IsCustomErrorCategorizationEnabled(envs map[string]string) bool {
	if val, ok := envs[CustomErrorCategorizationEnvVar]; ok && val == trueValue {
		return true
	}
	return false
}

// ConvertPath converts Unix-style paths to Windows format on Windows OS.
func ConvertPath(path string) string {
	if runtime.GOOS == windowsOS {
		path = strings.ReplaceAll(path, "/", "\\")
		if path != "" && path[0] == '\\' {
			path = "c:" + path
		}
	}
	return path
}

// GetStdoutLogFilePath returns the path for stdout log file used by custom error categorization.
func GetStdoutLogFilePath(stepID string) string {
	path := fmt.Sprintf("%s/%s/%s%s", pipeline.GetSharedVolPath(), HarnessInternalLogsDir, stepID, StdoutLogSuffix)
	return ConvertPath(path)
}

// GetStderrLogFilePath returns the path for stderr log file used by custom error categorization.
func GetStderrLogFilePath(stepID string) string {
	path := fmt.Sprintf("%s/%s/%s%s", pipeline.GetSharedVolPath(), HarnessInternalLogsDir, stepID, StderrLogSuffix)
	return ConvertPath(path)
}

// EnsureLogDirectory creates the log directory for error categorization if it doesn't exist.
func EnsureLogDirectory() error {
	logDir := fmt.Sprintf("%s/%s", pipeline.GetSharedVolPath(), HarnessInternalLogsDir)
	return os.MkdirAll(ConvertPath(logDir), LogDirPermissions)
}

// CreateLogFiles creates stdout and stderr log files for custom error categorization.
func CreateLogFiles(stepID string, envs map[string]string) *LogFileHandles {
	handles := &LogFileHandles{}

	if !IsCustomErrorCategorizationEnabled(envs) {
		return handles
	}

	if err := EnsureLogDirectory(); err != nil {
		logrus.Warnf("Failed to create log directory for custom error categorization: %v", err)
		return handles
	}

	// Create stdout log file
	handles.StdoutPath = GetStdoutLogFilePath(stepID)
	if f, err := os.Create(handles.StdoutPath); err != nil {
		logrus.Warnf("Failed to create stdout log file at %s: %v", handles.StdoutPath, err)
		handles.StdoutPath = ""
	} else {
		handles.StdoutFile = f
	}

	// Create stderr log file
	handles.StderrPath = GetStderrLogFilePath(stepID)
	if f, err := os.Create(handles.StderrPath); err != nil {
		logrus.Warnf("Failed to create stderr log file at %s: %v", handles.StderrPath, err)
		handles.StderrPath = ""
	} else {
		handles.StderrFile = f
	}

	return handles
}

// Close closes the log file handles if they are open.
func (h *LogFileHandles) Close() {
	if h.StdoutFile != nil {
		h.StdoutFile.Close()
	}
	if h.StderrFile != nil {
		h.StderrFile.Close()
	}
}

// Cleanup closes and removes the log files
func (h *LogFileHandles) Cleanup() {
	h.Close()

	if h.StdoutPath != "" {
		if err := os.Remove(h.StdoutPath); err != nil && !os.IsNotExist(err) {
			logrus.Warnf("Failed to cleanup stdout log file at %s: %v", h.StdoutPath, err)
		}
	}
	if h.StderrPath != "" {
		if err := os.Remove(h.StderrPath); err != nil && !os.IsNotExist(err) {
			logrus.Warnf("Failed to cleanup stderr log file at %s: %v", h.StderrPath, err)
		}
	}
}

// GetStdoutWriter returns an io.Writer that writes to both the original output and the stdout log file
func (h *LogFileHandles) GetStdoutWriter(originalOutput io.Writer) io.Writer {
	if h.StdoutFile != nil {
		return io.MultiWriter(originalOutput, h.StdoutFile)
	}
	return originalOutput
}

// GetStderrWriter returns an io.Writer that writes to both the original output and the stderr log file
func (h *LogFileHandles) GetStderrWriter(originalOutput io.Writer) io.Writer {
	if h.StderrFile != nil {
		return io.MultiWriter(originalOutput, h.StderrFile)
	}
	return originalOutput
}

// HasLogFiles returns true if either stdout or stderr log files were created.
func (h *LogFileHandles) HasLogFiles() bool {
	return h.StdoutFile != nil && h.StderrFile != nil
}
