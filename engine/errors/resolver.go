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
	// HarnessWorkspaceEnv is the environment variable for workspace path
	HarnessWorkspaceEnv = "HARNESS_WORKSPACE"
	// DefaultErrorsYAMLPath is the default path for errors.yaml file
	DefaultErrorsYAMLPath = ".harness/errors.yaml"
	// DefaultErrorsYMLPath is the alternative default path for errors.yml file
	DefaultErrorsYMLPath = ".harness/errors.yml"
)

// ResolveErrorsYAMLPath resolves the path to errors.yaml file from various sources
// Priority order:
// 1. HARNESS_ERRORS_YAML_PATH environment variable (from step envs or system env)
// 2. Default location: .harness/errors.yaml relative to workspace
func ResolveErrorsYAMLPath(step *api.StartStepRequest) (string, error) {
	stepID := step.ID

	// Check environment variable first (from step envs)
	if envPath := getEnvFromStepOrSystem(step.Envs, HarnessErrorsYAMLPathEnv); envPath != "" {
		if _, err := os.Stat(envPath); err == nil {
			logrus.WithFields(logrus.Fields{
				"path":    envPath,
				"step_id": stepID,
				"source":  "environment_variable",
			}).Infoln("TEST_YAML_RESOLVE: Path resolved from HARNESS_ERRORS_YAML_PATH env var")
			return envPath, nil
		}
		// If env path doesn't exist, fallback to default paths
		logrus.WithFields(logrus.Fields{
			"env_path": envPath,
			"step_id":  stepID,
		}).Infoln("TEST_YAML_RESOLVE: Env var path not found, falling back to defaults")
	}

	// Get workspace from HARNESS_WORKSPACE env var (where code is cloned)
	// This is the actual workspace path where git clone happens
	workspace := getEnvFromStepOrSystem(step.Envs, HarnessWorkspaceEnv)
	if workspace == "" {
		// Fallback to GetSharedVolPath for backwards compatibility
		workspace = pipeline.GetSharedVolPath()
		logrus.WithFields(logrus.Fields{
			"step_id":   stepID,
			"workspace": workspace,
		}).Debugln("TEST_YAML_RESOLVE: HARNESS_WORKSPACE not set, falling back to shared vol path")
	} else {
		logrus.WithFields(logrus.Fields{
			"step_id":   stepID,
			"workspace": workspace,
		}).Debugln("TEST_YAML_RESOLVE: Using HARNESS_WORKSPACE")
	}

	if workspace == "" {
		return "", fmt.Errorf("workspace path is empty")
	}

	// Try .yaml first
	yamlPath := filepath.Join(workspace, DefaultErrorsYAMLPath)
	if _, err := os.Stat(yamlPath); err == nil {
		logrus.WithFields(logrus.Fields{
			"path":    yamlPath,
			"step_id": stepID,
			"source":  "default_yaml",
		}).Infoln("TEST_YAML_RESOLVE: Path resolved from default location (.yaml)")
		return yamlPath, nil
	}

	// Try .yml as fallback
	ymlPath := filepath.Join(workspace, DefaultErrorsYMLPath)
	if _, err := os.Stat(ymlPath); err == nil {
		logrus.WithFields(logrus.Fields{
			"path":    ymlPath,
			"step_id": stepID,
			"source":  "default_yml",
		}).Infoln("TEST_YAML_RESOLVE: Path resolved from default location (.yml)")
		return ymlPath, nil
	}

	// Neither file exists, return error
	return "", fmt.Errorf("errors YAML file not found at default locations: %s or %s", yamlPath, ymlPath)
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
