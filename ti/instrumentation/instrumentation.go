// Copyright 2022 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package instrumentation

import (
	"context"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/harness/lite-engine/api"
	"github.com/harness/lite-engine/internal/filesystem"
	tiCfg "github.com/harness/lite-engine/ti/config"
	"github.com/harness/lite-engine/ti/instrumentation/common"
	"github.com/harness/lite-engine/ti/instrumentation/java"
	"github.com/harness/lite-engine/ti/instrumentation/python"
	"github.com/harness/lite-engine/ti/instrumentation/ruby"
	"github.com/harness/lite-engine/ti/testsplitter"
	telemetryutils "github.com/harness/ti-client/clientUtils/telemetryUtils"
	"github.com/harness/ti-client/types"    //nolint:staticcheck
	ti "github.com/harness/ti-client/types" //nolint:staticcheck // Aliased import used within this file
	"github.com/sirupsen/logrus"
)

const (
	classTimingTestSplitStrategy = testsplitter.SplitByClassTimeStr
	countTestSplitStrategy       = testsplitter.SplitByTestCount
	defaultTestSplitStrategy     = classTimingTestSplitStrategy
	JavaAgentJar                 = "java-agent.jar"
	AgentArg                     = "-javaagent:%s=%s"
	envHarnessTiCgVersion        = "HARNESS_TI_CG_VERSION"
	envHarnessMatrixAxis         = "HARNESS_MATRIX_AXIS"
)

func getTestSelection(ctx context.Context, runner TestRunner, config *api.RunTestConfig, fs filesystem.FileSystem,
	stepID, workspace string, log *logrus.Logger, isManual bool, tiConfig *tiCfg.Cfg) (testSelection ti.SelectTestsResp, moduleList []string) {
	selection := ti.SelectTestsResp{}
	if isManual {
		// Manual run
		log.Infoln("Detected manual execution - for test intelligence to be configured the execution should be via a PR or Push trigger, running all the tests.")
		config.RunOnlySelectedTests = false // run all the tests if it is a manual execution
		return selection, moduleList
	}
	defer func(config *api.RunTestConfig) {
		// Determine TI Feature state for Push / PR runs
		if tiConfig.GetParseSavings() {
			if config.RunOnlySelectedTests {
				// TI selected subset of tests
				tiConfig.WriteFeatureState(stepID, ti.TI, ti.OPTIMIZED)
			} else {
				// TI selected all tests or returned an error which resulted in full run
				tiConfig.WriteFeatureState(stepID, ti.TI, ti.FULL_RUN)
			}
		}
	}(config)

	// Push+Manual/PR execution
	var files []ti.File
	var err error
	if IsPushTriggerExecution(tiConfig) {
		lastSuccessfulCommitID, commitErr := GetCommitInfo(ctx, stepID, tiConfig)
		if commitErr != nil {
			log.Infoln("Failed to get reference commit", "error", commitErr)
			config.RunOnlySelectedTests = false // TI selected all the tests to be run
			return selection, moduleList
		}
		if lastSuccessfulCommitID != "" {
			log.Infoln("Using reference commit: ", lastSuccessfulCommitID)
			files, err = GetChangedFilesPush(ctx, workspace, lastSuccessfulCommitID, tiConfig.GetSha(), log)
			if err != nil {
				log.Errorln("Unable to get changed files list. Running all the tests.", "error", err)
				config.RunOnlySelectedTests = false
				return selection, moduleList
			}
		} else {
			// select all tests. It should still go to ti-service to update stats. This is full run bootstrap case
			log.Infoln("No reference commit found")
			config.RunOnlySelectedTests = false
		}
	} else {
		files, err = GetChangedFilesPR(ctx, workspace, log)
		if err != nil || len(files) == 0 {
			log.Errorln("Unable to get changed files list for PR. Running all the tests.", "error", err)
			config.RunOnlySelectedTests = false
			return selection, moduleList
		}
	}
	files, moduleList, _ = checkForBazelOptimization(ctx, workspace, fs, log, files)

	// Call TI svc only when there is a chance of running selected tests
	filesWithPkg := runner.ReadPackages(workspace, files)
	testGlobs, excludeGlobs := runner.GetTestGlobs()
	selection, err = SelectTests(ctx, workspace, filesWithPkg, config.RunOnlySelectedTests, stepID, testGlobs, fs, tiConfig, false)
	selection = filterTestsAfterSelection(selection, testGlobs, excludeGlobs)
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
	return selection, moduleList
}

// check if bazel optimization is enabled and call function to add new changed files to list
func checkForBazelOptimization(ctx context.Context, workspace string, fs filesystem.FileSystem, log *logrus.Logger, files []ti.File) ([]ti.File, []string, error) {
	var moduleList []string
	var newFiles []ti.File
	// check ticonfig params to allow bazel optimization, and get threshold for max file count
	tiConfigYaml, err := getTiConfig(workspace, fs)
	if err != nil {
		return files, moduleList, fmt.Errorf("failed to parse TI configuration file %v , skipping bazel optimization ", err)
	}

	// skip bazel src inspection if optimization in config not selected
	if tiConfigYaml.Config.BazelOptimization {
		// Validate  BazelFileCountThreshold to integer
		if tiConfigYaml.Config.BazelFileCountThreshold == 0 {
			return files, moduleList, fmt.Errorf("bazelFileCount not set in ticonfig.yml %v", err)
		}
		newFiles, moduleList, err = addBazelFilesToChangedFiles(ctx, workspace, log, files, tiConfigYaml.Config.BazelFileCountThreshold)
		if err != nil {
			return files, moduleList, fmt.Errorf("bazel optimazation failed due to error %v", err)
		}
		log.Infoln("Changed file list after bazel optimization: ", newFiles)
		log.Infoln("Changed module list after bazel optimization: ", moduleList)
		files = newFiles
	}
	return files, moduleList, nil
}

// ComputeSelectedTestsV2 updates TI selection depending on the split strategy
// AutoDetectTests output and parallelism configuration
func ComputeSelectedTestsV2(ctx context.Context, runConfigV2 *api.RunTestsV2Config, log *logrus.Logger,
	selection *ti.SelectTestsResp, stepID, workspace string, envs map[string]string, testGlobs []string, tiConfig *tiCfg.Cfg, runOnlySelectedTests bool, fs filesystem.FileSystem) bool {
	// Adding only this remove this condition later when we have complete specs
	if runOnlySelectedTests && len(selection.Tests) == 0 {
		// TI returned zero test cases to run. Skip parallelism as
		// there are no tests to run
		return runOnlySelectedTests
	}
	splitIdx, splitTotal := GetSplitIdxAndTotal(envs)
	tests := make([]ti.RunnableTest, 0)

	// Autodetection
	if !runOnlySelectedTests {
		// For full runs, detect all the tests in the repo and split them
		// If autodetect fails or detects no tests, we run all tests in step 0
		var err error
		tests, err = AutoDetectTests(ctx, workspace, testGlobs, log, envs, fs)
		if err != nil || len(tests) == 0 {
			// AutoDetectTests output should be same across all the parallel steps. If one of the step
			// receives error / no tests to run, all the other steps should have the same output
			if splitIdx == 0 {
				// Error while auto-detecting, run all tests for parallel step 0
				runOnlySelectedTests = false
				log.WithError(err).Errorf("Error in auto-detecting tests for splitting, running all tests")
				log.Debugf("Workspace %s with test globs %v", workspace, testGlobs)
			} else {
				// Error while auto-detecting, no tests for other parallel steps
				selection.Tests = []ti.RunnableTest{}
				runOnlySelectedTests = true
				log.WithError(err).Errorln("Error in auto-detecting tests for splitting, running all tests in parallel step 0")
				log.Debugf("Workspace %s with test globs %v", workspace, testGlobs)
			}
			return runOnlySelectedTests
		}
		// Auto-detected tests successfully
		log.Infoln(fmt.Sprintf("Autodetected tests: %s", formatTests(tests)))
	} else if len(selection.Tests) > 0 {
		// In case of intelligent runs, split the tests from TI SelectTests API response
		tests = selection.Tests
	}
	// Split the tests and send the split slice to the runner
	splitTests, err := getSplitTests(ctx, log, tests, stepID, defaultTestSplitStrategy, splitIdx, splitTotal, tiConfig)
	if err != nil {
		// Error while splitting by input strategy, splitting tests equally
		log.Errorln("Error occurred while splitting the tests by default split strategy. Splitting tests equally")
		splitTests, _ = getSplitTests(ctx, log, tests, stepID, countTestSplitStrategy, splitIdx, splitTotal, tiConfig)
	}
	log.Infoln(fmt.Sprintf("Test split for this run: %s", formatTests(splitTests)))

	// Modify selections to run only selected tests
	selection.Tests = splitTests
	return true
}

// computeSelectedTests updates TI selection and ignoreInstr in-place depending on the
// AutoDetectTests output and parallelism configuration
func computeSelectedTests(ctx context.Context, config *api.RunTestConfig, log *logrus.Logger, runner TestRunner,
	selection *ti.SelectTestsResp, stepID, workspace string, envs map[string]string, tiConfig *tiCfg.Cfg) {
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

	splitIdx, splitTotal := GetSplitIdxAndTotal(envs)

	tests := make([]ti.RunnableTest, 0)
	if !config.RunOnlySelectedTests {
		// For full runs, detect all the tests in the repo and split them
		// If autodetect fails or detects no tests, we run all tests in step 0
		var err error
		testGlobs := sanitizeTestGlob(config.TestGlobs)
		tests, err = runner.AutoDetectTests(ctx, workspace, testGlobs)
		if err != nil || len(tests) == 0 {
			// AutoDetectTests output should be same across all the parallel steps. If one of the step
			// receives error / no tests to run, all the other steps should have the same output
			if splitIdx == 0 {
				// Error while auto-detecting, run all tests for parallel step 0
				config.RunOnlySelectedTests = false
				log.WithError(err).Errorf("Error in auto-detecting tests for splitting, running all tests")
				log.Debugf("Workspace %s with test globs %v", workspace, testGlobs)
			} else {
				// Error while auto-detecting, no tests for other parallel steps
				selection.Tests = []ti.RunnableTest{}
				config.RunOnlySelectedTests = true
				log.WithError(err).Errorln("Error in auto-detecting tests for splitting, running all tests in parallel step 0")
				log.Debugf("Workspace %s with test globs %v", workspace, testGlobs)
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
	splitTests, err := getSplitTests(ctx, log, tests, stepID, config.TestSplitStrategy, splitIdx, splitTotal, tiConfig)
	if err != nil {
		// Error while splitting by input strategy, splitting tests equally
		log.Errorln("Error occurred while splitting the tests by input strategy. Splitting tests equally")
		splitTests, _ = getSplitTests(ctx, log, tests, stepID, countTestSplitStrategy, splitIdx, splitTotal, tiConfig)
	}
	log.Infoln(fmt.Sprintf("Test split for this run: %s", formatTests(splitTests)))

	// Modify runner input to run selected tests
	selection.Tests = splitTests
	config.RunOnlySelectedTests = true
}

func GetCmd(
	ctx context.Context,
	config *api.RunTestConfig,
	stepID, workspace string,
	uniqueStepID string,
	log *logrus.Logger,
	envs map[string]string,
	cfg *tiCfg.Cfg,
	testMetadata *types.TestIntelligenceMetaData,
) (string, error) {
	fs := filesystem.New()
	tmpFilePath := filepath.Join(cfg.GetDataDir(), GetUniqueHash(uniqueStepID, cfg))

	if config.TestSplitStrategy == "" {
		config.TestSplitStrategy = defaultTestSplitStrategy
	}

	// Ignore instrumentation when it's a manual run or user has unchecked RunOnlySelectedTests option
	isManual := IsManualExecution(cfg)
	ignoreInstr := isManual || !config.RunOnlySelectedTests
	cfg.SetIgnoreInstr(ignoreInstr)
	if cfg.GetIgnoreInstr() {
		config.RunOnlySelectedTests = false
	}

	// Get TI runner
	config.Language = strings.ToLower(config.Language)
	config.BuildTool = strings.ToLower(config.BuildTool)
	testGlobs := sanitizeTestGlob(config.TestGlobs)
	runner, useYaml, err := getTiRunner(config.Language, config.BuildTool, log, fs, testGlobs, envs)
	if err != nil {
		return "", err
	}
	var modules []string
	selection := ti.SelectTestsResp{}
	var artifactDir, iniFilePath string
	if !cfg.GetIgnoreInstr() {
		// Get the tests and module test targets that need to be run if we are running selected tests
		selection, modules = getTestSelection(ctx, runner, config, fs, stepID, workspace, log, isManual, cfg)
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
		computeSelectedTests(ctx, config, log, runner, &selection, stepID, workspace, envs, cfg)
	}

	// set runnerArg for bazel runner
	runnerArgs := common.RunnerArgs{}
	runnerArgs.ModuleList = modules

	testCmd, err := runner.GetCmd(ctx, selection.Tests, config.Args, workspace, iniFilePath, artifactDir, cfg.GetIgnoreInstr(), !config.RunOnlySelectedTests, runnerArgs)
	if err != nil {
		return "", err
	}
	testMetadata.TotalSelectedTests = selection.SelectedTests
	testMetadata.TotalSelectedTestClass = telemetryutils.CountDistinctSelectedClasses(selection.Tests)

	if cfg.GetIgnoreInstr() {
		log.Infoln("Ignoring instrumentation and not attaching agent")
	}

	command := fmt.Sprintf("%s\n%s\n%s", config.PreCommand, testCmd, config.PostCommand)
	return command, nil
}

// InjectReportInformation add default test paths information to ruby and python when test runner is invoked without a value
// This serves as a default
func InjectReportInformation(r *api.StartStepRequest) {
	switch strings.ToLower(r.RunTest.Language) {
	case "ruby", "python":
		if r.RunTest.Args == "" && len(r.TestReport.Junit.Paths) == 0 {
			r.TestReport.Junit.Paths = []string{fmt.Sprintf("**/%s*", common.HarnessDefaultReportPath)}
			r.TestReport.Kind = api.Junit
		}
	}
}

func sanitizeTestGlob(globString string) []string {
	if globString == "" {
		return make([]string, 0)
	}
	return strings.Split(globString, ",")
}

func AutoDetectTests(ctx context.Context, workspace string, testGlobs []string, log *logrus.Logger, envs map[string]string, fs filesystem.FileSystem) ([]ti.RunnableTest, error) {
	tests, err := java.AutoDetectTests(ctx, workspace, testGlobs)
	if err != nil {
		log.WithError(err).Errorln("Could not auto-detect java tests")
	}
	log.Debugf("Detected Java tests: %v", tests)

	rubyRunner := ruby.NewRubyRunner(log, fs, testGlobs, envs)
	rubyTests, err := rubyRunner.AutoDetectTestsV2(ctx, workspace, testGlobs)
	if err != nil {
		log.WithError(err).Errorln("Could not auto-detect ruby tests")
	}
	log.Debugf("Detected Ruby tests: %v", rubyTests)
	tests = append(tests, rubyTests...)

	pyRunner := python.NewPytestRunner(log, fs, testGlobs)
	pyTests, err := pyRunner.AutoDetectTests(ctx, workspace, testGlobs)
	if err != nil {
		log.WithError(err).Errorln("Could not auto-detect python tests")
	}
	log.Debugf("Detected Python tests: %v", pyTests)
	tests = append(tests, pyTests...)
	return tests, nil
}

func IsRerunFailedTestsEnabled(envs map[string]string) bool {
	return envs[ciTiRerunFailedTestFF] == "true"
}
