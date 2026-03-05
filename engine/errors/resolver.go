// Copyright 2026 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

// Package errors provides custom error categorization utilities for lite-engine.
package errors

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/harness/lite-engine/api"
	"github.com/harness/lite-engine/pipeline"
	"github.com/sirupsen/logrus"
)

// Environment variable names for error categorization
const (
	HarnessErrorsYAMLPathEnv = "HARNESS_ERRORS_YAML_PATH"
	DroneWorkspaceEnv        = "DRONE_WORKSPACE"
	HarnessWorkspaceEnv      = "HARNESS_WORKSPACE"
	DefaultErrorsYAMLPath    = ".harness/errors.yaml"
	DefaultErrorsYMLPath     = ".harness/errors.yml"
)

// ResolveErrorsYAMLPath resolves the errors YAML path with priority:
// 1. Pipeline envs, 2. Step envs, 3. System env, 4. Default paths
func ResolveErrorsYAMLPath(step *api.StartStepRequest, pipelineEnvs map[string]string) (string, error) {
	// Priority 1: Pipeline/Stage envs
	if path := getEnv(pipelineEnvs, HarnessErrorsYAMLPathEnv); path != "" {
		if _, err := os.Stat(path); err == nil {
			return path, nil
		}
	}

	// Priority 2: Step envs
	if path := getEnv(step.Envs, HarnessErrorsYAMLPathEnv); path != "" {
		if _, err := os.Stat(path); err == nil {
			return path, nil
		}
	}

	// Priority 3: System env
	if path := os.Getenv(HarnessErrorsYAMLPathEnv); path != "" {
		if _, err := os.Stat(path); err == nil {
			return path, nil
		}
	}

	// Priority 4: Default paths in workspace
	workspace := resolveWorkspace(step, pipelineEnvs)
	if workspace == "" {
		return "", fmt.Errorf("workspace path is empty")
	}

	// Try .yaml first
	yamlPath := filepath.Join(workspace, DefaultErrorsYAMLPath)
	if _, err := os.Stat(yamlPath); err == nil {
		return yamlPath, nil
	}

	// Try .yml as fallback
	ymlPath := filepath.Join(workspace, DefaultErrorsYMLPath)
	if _, err := os.Stat(ymlPath); err == nil {
		return ymlPath, nil
	}

	return "", fmt.Errorf("errors YAML file not found at default locations: %s or %s", yamlPath, ymlPath)
}

// resolveWorkspace determines the workspace path
func resolveWorkspace(step *api.StartStepRequest, pipelineEnvs map[string]string) string {
	if workspace := getEnv(step.Envs, DroneWorkspaceEnv); workspace != "" {
		return workspace
	}

	if workspace := getEnv(pipelineEnvs, DroneWorkspaceEnv); workspace != "" {
		return workspace
	}

	if workspace := getEnv(step.Envs, HarnessWorkspaceEnv); workspace != "" {
		return workspace
	}

	if workspace := getEnv(pipelineEnvs, HarnessWorkspaceEnv); workspace != "" {
		return workspace
	}

	if step.WorkingDir != "" {
		return step.WorkingDir
	}

	sharedPath := pipeline.GetSharedVolPath()
	if sharedPath != "" {
		logrus.WithFields(logrus.Fields{
			"workspace": sharedPath,
		}).Debugln("Using SharedVolPath as fallback")
		return sharedPath
	}

	return ""
}

// getEnv safely gets a value from a map
func getEnv(envs map[string]string, key string) string {
	if envs != nil {
		if val, ok := envs[key]; ok && val != "" {
			return val
		}
	}
	return ""
}
