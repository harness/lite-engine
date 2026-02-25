// Copyright 2026 Harness Inc. All rights reserved.
// Use of this source code is governed by the PolyForm Free Trial 1.0.0 license
// that can be found in the licenses directory at the root of this repository, also available at
// https://polyformproject.org/wp-content/uploads/2020/05/PolyForm-Free-Trial-1.0.0.txt.

package errors

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/harness/lite-engine/api"
	"github.com/harness/lite-engine/pipeline"
	"github.com/sirupsen/logrus"
)

const (
	// HarnessErrorsYAMLPathEnv is the environment variable name for errors YAML path
	HarnessErrorsYAMLPathEnv = "HARNESS_ERRORS_YAML_PATH"
	// DroneWorkspaceEnv is the environment variable for drone workspace (set by drone-runner-aws)
	DroneWorkspaceEnv = "DRONE_WORKSPACE"
	// HarnessWorkspaceEnv is the environment variable for workspace path
	HarnessWorkspaceEnv = "HARNESS_WORKSPACE"
	// DefaultErrorsYAMLPath is the default path for errors.yaml file
	DefaultErrorsYAMLPath = ".harness/errors.yaml"
	// DefaultErrorsYMLPath is the alternative default path for errors.yml file
	DefaultErrorsYMLPath = ".harness/errors.yml"
)

// ResolveErrorsYAMLPath resolves the path to errors.yaml file from various sources
// Priority order:
//  1. HARNESS_ERRORS_YAML_PATH environment variable (custom path)
//  2. Default location relative to workspace, where workspace is resolved from:
//     a. DRONE_WORKSPACE (from pipeline config envs, set by drone-runner-aws)
//     b. HARNESS_WORKSPACE
//     c. step.WorkingDir (set by CI-Manager)
//     d. pipeline.GetSharedVolPath() (fallback)
func ResolveErrorsYAMLPath(step *api.StartStepRequest) (string, error) {
	stepID := step.ID

	// Check custom path environment variable first
	if envPath := getEnvFromStepOrSystem(step.Envs, HarnessErrorsYAMLPathEnv); envPath != "" {
		if _, err := os.Stat(envPath); err == nil {
			logrus.WithFields(logrus.Fields{
				"path":    envPath,
				"step_id": stepID,
				"source":  "HARNESS_ERRORS_YAML_PATH",
			}).Infoln("Resolved errors YAML path from custom env var")
			return envPath, nil
		}
		// If env path doesn't exist, fallback to default paths
		logrus.WithFields(logrus.Fields{
			"env_path": envPath,
			"step_id":  stepID,
		}).Debugln("Custom YAML path not found, falling back to defaults")
	}

	// Resolve workspace with priority: DRONE_WORKSPACE > HARNESS_WORKSPACE > WorkingDir > SharedVolPath
	workspace := resolveWorkspace(step, stepID)

	if workspace == "" {
		return "", fmt.Errorf("workspace path is empty")
	}

	// Try .yaml first
	yamlPath := filepath.Join(workspace, DefaultErrorsYAMLPath)
	if _, err := os.Stat(yamlPath); err == nil {
		logrus.WithFields(logrus.Fields{
			"path":    yamlPath,
			"step_id": stepID,
		}).Infoln("Resolved errors YAML path")
		return yamlPath, nil
	}

	// Try .yml as fallback
	ymlPath := filepath.Join(workspace, DefaultErrorsYMLPath)
	if _, err := os.Stat(ymlPath); err == nil {
		logrus.WithFields(logrus.Fields{
			"path":    ymlPath,
			"step_id": stepID,
		}).Infoln("Resolved errors YAML path (.yml)")
		return ymlPath, nil
	}

	// Neither file exists, return error
	return "", fmt.Errorf("errors YAML file not found at default locations: %s or %s", yamlPath, ymlPath)
}

// resolveWorkspace determines the workspace path using priority order:
// 1. DRONE_WORKSPACE (from pipeline config envs, merged by engine.Run)
// 2. HARNESS_WORKSPACE
// 3. step.WorkingDir (set by CI-Manager to /harness for hosted VMs)
// 4. pipeline.GetSharedVolPath() (fallback)
func resolveWorkspace(step *api.StartStepRequest, stepID string) string {
	// Priority 1: DRONE_WORKSPACE (from pipeline config envs, available after engine.Run merges them)
	if workspace := getEnvFromStepOrSystem(step.Envs, DroneWorkspaceEnv); workspace != "" {
		logrus.WithFields(logrus.Fields{
			"step_id":   stepID,
			"workspace": workspace,
			"source":    "DRONE_WORKSPACE",
		}).Debugln("Using DRONE_WORKSPACE for errors YAML resolution")
		return workspace
	}

	// Priority 2: HARNESS_WORKSPACE
	if workspace := getEnvFromStepOrSystem(step.Envs, HarnessWorkspaceEnv); workspace != "" {
		logrus.WithFields(logrus.Fields{
			"step_id":   stepID,
			"workspace": workspace,
			"source":    "HARNESS_WORKSPACE",
		}).Debugln("Using HARNESS_WORKSPACE for errors YAML resolution")
		return workspace
	}

	// Priority 3: step.WorkingDir (set by CI-Manager)
	if step.WorkingDir != "" {
		logrus.WithFields(logrus.Fields{
			"step_id":   stepID,
			"workspace": step.WorkingDir,
			"source":    "WorkingDir",
		}).Debugln("Using step WorkingDir for errors YAML resolution")
		return step.WorkingDir
	}

	// Priority 4: Fallback to shared vol path
	workspace := pipeline.GetSharedVolPath()
	logrus.WithFields(logrus.Fields{
		"step_id":   stepID,
		"workspace": workspace,
		"source":    "SharedVolPath",
	}).Debugln("Using shared vol path fallback for errors YAML resolution")
	return workspace
}

// getEnvFromStepOrSystem gets an environment variable value from step envs first,
// then falls back to system environment variable
func getEnvFromStepOrSystem(stepEnvs map[string]string, key string) string {
	// Check step envs first
	if stepEnvs != nil {
		if val, ok := stepEnvs[key]; ok && val != "" {
			return val
		}
	}
	// Fall back to system environment variable
	return os.Getenv(key)
}
