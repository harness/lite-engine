// Copyright 2022 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package instrumentation

import (
	"context"
	"fmt"
	"runtime"
	"strings"

	"github.com/sirupsen/logrus"

	"github.com/harness/lite-engine/api"
	"github.com/harness/lite-engine/internal/filesystem"
	tiCfg "github.com/harness/lite-engine/ti/config"
	"github.com/harness/lite-engine/ti/testsplitter"
	ti "github.com/harness/ti-client/types"
)

const (
	classTimingTestSplitStrategy = testsplitter.SplitByClassTimeStr
	countTestSplitStrategy       = testsplitter.SplitByTestCount
	defaultTestSplitStrategy     = classTimingTestSplitStrategy
)

func getTestSelection(ctx context.Context, runner TestRunner, config *api.RunTestConfig, fs filesystem.FileSystem,
	stepID, workspace string, log *logrus.Logger, isManual bool, tiConfig *tiCfg.Cfg) ti.SelectTestsResp {
	selection := ti.SelectTestsResp{}
	if isManual {
		// Manual run
		log.Infoln("Detected manual execution - for test intelligence to be configured the execution should be via a PR or Push trigger, running all the tests.")
		config.RunOnlySelectedTests = false // run all the tests if it is a manual execution
		return selection
	}
	// Push+Manual/PR execution
	var files []ti.File
	var err error
	if IsPushTriggerExecution(tiConfig) {
		lastSuccessfulCommitID, commitErr := getCommitInfo(ctx, stepID, tiConfig)
		if commitErr != nil {
			log.Infoln("Failed to get reference commit", "error", commitErr)
			config.RunOnlySelectedTests = false // TI selected all the tests to be run
			return selection
		}
		if lastSuccessfulCommitID == "" {
			log.Infoln("Test Intelligence determined to run all the tests to bootstrap")
			config.RunOnlySelectedTests = false // TI selected all the tests to be run
			return selection
		}
		log.Infoln("Using reference commit: ", lastSuccessfulCommitID)
		files, err = getChangedFilesPush(ctx, workspace, lastSuccessfulCommitID, tiConfig.GetSha(), log)
		if err != nil {
			log.Errorln("Unable to get changed files list. Running all the tests.", "error", err)
			config.RunOnlySelectedTests = false
			return selection
		}
	} else {
		files, err = getChangedFilesPR(ctx, workspace, log)
		if err != nil || len(files) == 0 {
			log.Errorln("Unable to get changed files list for PR. Running all the tests.", "error", err)
			config.RunOnlySelectedTests = false
			return selection
		}
	}
	// Call TI svc only when there is a chance of running selected tests
	filesWithPkg := runner.ReadPackages(workspace, files)
	selection, err = selectTests(ctx, workspace, filesWithPkg, config.RunOnlySelectedTests, stepID, fs, tiConfig)
	testGlobs := strings.Split(config.TestGlobs, ",")
	selection = filterTestsAfterSelection(selection, testGlobs)
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
	return selection
}

// computeSelectedTests updates TI selection and ignoreInstr in-place depending on the
// AutoDetectTests output and parallelism configuration
func computeSelectedTests(ctx context.Context, config *api.RunTestConfig, log *logrus.Logger, runner TestRunner,
	selection *ti.SelectTestsResp, workspace string, envs map[string]string, tiConfig *tiCfg.Cfg) {
	if !config.ParallelizeTests {
		log.Infoln("Skipping test splitting as requested")
		return
	}
	if config.RunOnlySelectedTests && len(selection.Tests) == 0 {
		// TI returned zero test cases to run. Skip parallelism as
		// there are no tests to run
		return
	}
	log.Infoln("Splitting the tests as parallelism is enabled")

	stepIdx, _ := GetStepStrategyIteration(envs)
	stepTotal, _ := GetStepStrategyIterations(envs)
	if !IsStepParallelismEnabled(envs) {
		stepIdx = 0
		stepTotal = 1
	}
	stageIdx, _ := GetStageStrategyIteration(envs)
	stageTotal, _ := GetStageStrategyIterations(envs)
	if !IsStageParallelismEnabled(envs) {
		stageIdx = 0
		stageTotal = 1
	}
	splitIdx := stepTotal*stageIdx + stepIdx
	splitTotal := stepTotal * stageTotal

	tests := make([]ti.RunnableTest, 0)
	if !config.RunOnlySelectedTests {
		// For full runs, detect all the tests in the repo and split them
		// If autodetect fails or detects no tests, we run all tests in step 0
		var err error
		testGlobs := strings.Split(config.TestGlobs, ",")
		tests, err = runner.AutoDetectTests(ctx, workspace, testGlobs)
		if err != nil || len(tests) == 0 {
			// AutoDetectTests output should be same across all the parallel steps. If one of the step
			// receives error / no tests to run, all the other steps should have the same output
			if splitIdx == 0 {
				// Error while auto-detecting, run all tests for parallel step 0
				config.RunOnlySelectedTests = false
				log.Errorln("Error in auto-detecting tests for splitting, running all tests")
			} else {
				// Error while auto-detecting, no tests for other parallel steps
				selection.Tests = []ti.RunnableTest{}
				config.RunOnlySelectedTests = true
				log.Errorln("Error in auto-detecting tests for splitting, running all tests in parallel step 0")
			}
			return
		}
		// Auto-detected tests successfully
		log.Infoln(fmt.Sprintf("Autodetected tests: %s", formatTests(tests)))
	} else if len(selection.Tests) > 0 {
		// In case of intelligent runs, split the tests from TI SelectTests API response
		tests = selection.Tests
	}

	// Split the tests and send the split slice to the runner
	splitTests, err := getSplitTests(ctx, log, tests, config.TestSplitStrategy, splitIdx, splitTotal, tiConfig)
	if err != nil {
		// Error while splitting by input strategy, splitting tests equally
		log.Errorln("Error occurred while splitting the tests by input strategy. Splitting tests equally")
		splitTests, _ = getSplitTests(ctx, log, tests, countTestSplitStrategy, splitIdx, splitTotal, tiConfig)
	}
	log.Infoln(fmt.Sprintf("Test split for this run: %s", formatTests(splitTests)))

	// Modify runner input to run selected tests
	selection.Tests = splitTests
	config.RunOnlySelectedTests = true
}

func GetCmd(ctx context.Context, config *api.RunTestConfig, stepID, workspace string, log *logrus.Logger, envs map[string]string, cfg *tiCfg.Cfg) (string, error) {
	fs := filesystem.New()
	tmpFilePath := cfg.GetDataDir()

	if config.TestSplitStrategy == "" {
		config.TestSplitStrategy = defaultTestSplitStrategy
	}

	// Ignore instrumentation when it's a manual run or user has unchecked RunOnlySelectedTests option
	isManual := IsManualExecution(cfg)
	ignoreInstr := isManual || !config.RunOnlySelectedTests
	cfg.SetIgnoreInstr(ignoreInstr)

	// Get TI runner
	config.Language = strings.ToLower(config.Language)
	config.BuildTool = strings.ToLower(config.BuildTool)
	runner, useYaml, err := getTiRunner(config.Language, config.BuildTool, log, fs, strings.Split(config.TestGlobs, ","))
	if err != nil {
		return "", err
	}

	selection := ti.SelectTestsResp{}
	var artifactDir, iniFilePath string
	if !cfg.GetIgnoreInstr() {
		// Get the tests that need to be run if we are running selected tests
		selection = getTestSelection(ctx, runner, config, fs, stepID, workspace, log, isManual, cfg)

		// Install agent artifacts if not present
		artifactDir, err = installAgents(ctx, tmpFilePath, config.Language, runtime.GOOS, runtime.GOARCH, config.BuildTool, fs, log, cfg)
		if err != nil {
			return "", err
		}

		// Create the config file required for instrumentation
		// Ruby does not use config file now. Will add in the future
		// TODO: Ruby to use config file as well, remove both conditons
		if !strings.EqualFold(config.Language, "ruby") {
			iniFilePath, err = createConfigFile(runner, config.Packages, config.TestAnnotations, workspace, tmpFilePath, fs, log, useYaml)
			if err != nil {
				return "", err
			}
		} else {
			config.PreCommand = fmt.Sprintf("export TI_OUTPUT_PATH=%s\n%s", getCgDir(tmpFilePath), config.PreCommand)
		}
	}

	// Test splitting: only when parallelism is enabled
	if IsParallelismEnabled(envs) {
		computeSelectedTests(ctx, config, log, runner, &selection, workspace, envs, cfg)
	}

	testCmd, err := runner.GetCmd(ctx, selection.Tests, config.Args, workspace, iniFilePath, artifactDir, cfg.GetIgnoreInstr(), !config.RunOnlySelectedTests)
	if err != nil {
		return "", err
	}

	if cfg.GetIgnoreInstr() {
		log.Infoln("Ignoring instrumentation and not attaching agent")
	}

	command := fmt.Sprintf("%s\n%s\n%s", config.PreCommand, testCmd, config.PostCommand)
	return command, nil
}
