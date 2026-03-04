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
	HarnessInternalCacheDir         = ".harness-internal/cache"
	StdoutLogSuffix                 = "-stdout.log"
	StderrLogSuffix                 = "-stderr.log"
	DirPermissions                  = 0777
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

// IsCustomErrorCategorizationEnabled checks if the feature is enabled
func IsCustomErrorCategorizationEnabled(envs map[string]string) bool {
	val, ok := envs[CustomErrorCategorizationEnvVar]
	return ok && val == trueValue
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

// GetStdoutLogFilePath returns the path for stdout log file
func GetStdoutLogFilePath(stepID string) string {
	path := fmt.Sprintf("%s/%s/%s%s", pipeline.GetSharedVolPath(), HarnessInternalLogsDir, stepID, StdoutLogSuffix)
	return ConvertPath(path)
}

// GetStderrLogFilePath returns the path for stderr log file
func GetStderrLogFilePath(stepID string) string {
	path := fmt.Sprintf("%s/%s/%s%s", pipeline.GetSharedVolPath(), HarnessInternalLogsDir, stepID, StderrLogSuffix)
	return ConvertPath(path)
}

// EnsureLogDirectory creates the log directory if it doesn't exist
func EnsureLogDirectory() error {
	logDir := fmt.Sprintf("%s/%s", pipeline.GetSharedVolPath(), HarnessInternalLogsDir)
	return os.MkdirAll(ConvertPath(logDir), DirPermissions)
}

// GetCacheDir returns the cache directory path
func GetCacheDir() string {
	path := fmt.Sprintf("%s/%s", pipeline.GetSharedVolPath(), HarnessInternalCacheDir)
	return ConvertPath(path)
}

// EnsureCacheDirectory creates the cache directory if it doesn't exist
func EnsureCacheDirectory() error {
	cacheDir := fmt.Sprintf("%s/%s", pipeline.GetSharedVolPath(), HarnessInternalCacheDir)
	return os.MkdirAll(ConvertPath(cacheDir), DirPermissions)
}

// CreateLogFiles creates stdout and stderr log files for custom error categorization
func CreateLogFiles(stepID string, envs map[string]string) *LogFileHandles {
	handles := &LogFileHandles{}

	if !IsCustomErrorCategorizationEnabled(envs) {
		return handles
	}

	if err := EnsureLogDirectory(); err != nil {
		logrus.Warnf("Failed to create log directory for custom error categorization: %v", err)
		return handles
	}

	handles.StdoutPath = GetStdoutLogFilePath(stepID)
	if f, err := os.Create(handles.StdoutPath); err != nil {
		logrus.Warnf("Failed to create stdout log file at %s: %v", handles.StdoutPath, err)
		handles.StdoutPath = ""
	} else {
		handles.StdoutFile = f
	}

	handles.StderrPath = GetStderrLogFilePath(stepID)
	if f, err := os.Create(handles.StderrPath); err != nil {
		logrus.Warnf("Failed to create stderr log file at %s: %v", handles.StderrPath, err)
		handles.StderrPath = ""
	} else {
		handles.StderrFile = f
	}

	return handles
}

// Close closes the log file handles
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

// CleanupLogFiles removes log files for a step. Safe to call if files don't exist.
func CleanupLogFiles(stepID string) {
	stdoutPath := GetStdoutLogFilePath(stepID)
	if err := os.Remove(stdoutPath); err != nil && !os.IsNotExist(err) {
		logrus.Warnf("Failed to cleanup stdout log file at %s: %v", stdoutPath, err)
	}

	stderrPath := GetStderrLogFilePath(stepID)
	if err := os.Remove(stderrPath); err != nil && !os.IsNotExist(err) {
		logrus.Warnf("Failed to cleanup stderr log file at %s: %v", stderrPath, err)
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

// HasLogFiles returns true if either stdout or stderr log files were created
func (h *LogFileHandles) HasLogFiles() bool {
	return h.StdoutFile != nil && h.StderrFile != nil
}
