// Copyright 2022 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package instrumentation

import (
	"context"
	"fmt"
	"io"
	"runtime"
	"strings"

	"github.com/sirupsen/logrus"

	"github.com/harness/lite-engine/api"
	"github.com/harness/lite-engine/internal/filesystem"
	"github.com/harness/lite-engine/pipeline"
	"github.com/harness/lite-engine/ti"
	"github.com/harness/lite-engine/ti/instrumentation/csharp"
	"github.com/harness/lite-engine/ti/instrumentation/java"
)

func getTestSelection(ctx context.Context, config *api.RunTestConfig, fs filesystem.FileSystem, stepID, workspace string, log *logrus.Logger, isManual bool) ti.SelectTestsResp {
	selection := ti.SelectTestsResp{}
	if isManual {
		// Manual run
		log.Infoln("Detected manual execution - for test intelligence to be configured, a PR must be raised. Running all the tests.")
		config.RunOnlySelectedTests = false // run all the tests if it is a manual execution
	} else {
		// PR execution
		files, err := getChangedFiles(ctx, workspace, log)
		if err != nil || len(files) == 0 {
			log.Errorln("Unable to get changed files list")
			config.RunOnlySelectedTests = false
		} else {
			// PR execution: Call TI svc only when there is a chance of running selected tests
			selection, err = selectTests(ctx, workspace, files, config.RunOnlySelectedTests, stepID, fs)
			if err != nil {
				log.WithError(err).Errorln("There was some issue in trying to intelligently figure out tests to run. Running all the tests")
				config.RunOnlySelectedTests = false // run all the tests if an error was encountered
			} else if !valid(selection.Tests) { // This shouldn't happen
				log.Warnln("Test Intelligence did not return suitable tests")
				config.RunOnlySelectedTests = false // TI did not return suitable tests
			} else if selection.SelectAll {
				log.Infoln("Test Intelligence determined to run all the tests")
				config.RunOnlySelectedTests = false // TI selected all the tests to be run
			} else {
				log.Infoln(fmt.Sprintf("Running tests selected by Test Intelligence: %s", selection.Tests))
			}
		}
	}
	return selection
}

func GetCmd(ctx context.Context, config *api.RunTestConfig, stepID, workspace string, out io.Writer) (string, error) {
	fs := filesystem.New()
	tmpFilePath := pipeline.SharedVolPath
	log := logrus.New()
	log.Out = out

	// Get the tests that need to be run if we are running selected tests
	isManual := IsManualExecution()
	selection := getTestSelection(ctx, config, fs, stepID, workspace, log, isManual)

	var runner TestRunner
	useYaml := false
	config.Language = strings.ToLower(config.Language)
	config.BuildTool = strings.ToLower(config.BuildTool)
	switch strings.ToLower(config.Language) {
	case "scala", "java", "kotlin":
		useYaml = false
		switch config.BuildTool {
		case "maven":
			runner = java.NewMavenRunner(log, fs)
		case "gradle":
			runner = java.NewGradleRunner(log, fs)
		case "bazel":
			runner = java.NewBazelRunner(log, fs)
		case "sbt":
			{
				if config.Language != "scala" {
					return "", fmt.Errorf("build tool: SBT is not supported for non-Scala languages")
				}
				runner = java.NewSBTRunner(log, fs)
			}
		default:
			return "", fmt.Errorf("build tool: %s is not supported for Java", config.BuildTool)
		}
	case "csharp":
		useYaml = true
		switch config.BuildTool {
		case "dotnet":
			runner = csharp.NewDotnetRunner(log, fs)
		case "nunitconsole":
			runner = csharp.NewNunitConsoleRunner(log, fs)
		default:
			return "", fmt.Errorf("could not figure out the build tool: %s", config.BuildTool)
		}
	default:
		return "", fmt.Errorf("language %s is not suported", config.Language)
	}

	// Install agent artifacts if not present
	artifactDir, err := installAgents(ctx, tmpFilePath, config.Language, runtime.GOOS, runtime.GOARCH, config.BuildTool, fs, log)
	if err != nil {
		return "", err
	}

	// Create the config file required for instrumentation
	iniFilePath, err := createConfigFile(runner, config.Packages, config.TestAnnotations, workspace, tmpFilePath, fs, log, useYaml)
	if err != nil {
		return "", err
	}

	testCmd, err := runner.GetCmd(ctx, selection.Tests, config.Args, workspace, iniFilePath, artifactDir, isManual, !config.RunOnlySelectedTests)
	if err != nil {
		return "", err
	}

	// TODO: (Vistaar) If using this code for non-Windows, we might need to set TMPDIR for bazel
	command := fmt.Sprintf("%s\n%s\n%s", config.PreCommand, testCmd, config.PostCommand)
	return command, nil
}
