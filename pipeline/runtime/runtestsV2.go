// Copyright 2024 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package runtime

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/drone/runner-go/pipeline/runtime"
	"github.com/harness/lite-engine/api"
	"github.com/harness/lite-engine/internal/filesystem"
	"github.com/harness/lite-engine/pipeline"
	tiCfg "github.com/harness/lite-engine/ti/config"
	"github.com/harness/lite-engine/ti/instrumentation"
	"github.com/harness/lite-engine/ti/instrumentation/java"
	"github.com/harness/lite-engine/ti/instrumentation/python"
	"github.com/harness/lite-engine/ti/instrumentation/ruby"
	"github.com/harness/lite-engine/ti/savings"
	filter "github.com/harness/lite-engine/ti/testsfilteration"
	"github.com/harness/ti-client/types"
	"github.com/sirupsen/logrus"
)

const (
	outDir          = "%s/ti/v2/callgraph/cg/" // path passed as outDir in the config.ini file
	javaAgentV2Arg  = "-javaagent:%s=%s"
	javaAgentV2Jar  = "java-agent.jar"
	javaAgentV2Path = "/java/v2/"
	filterV2Dir     = "%s/ti/v2/filter"
	configV2Dir     = "%s/ti/v2/java/config"
)

// Ignoring optimization state for now
//
//nolint:funlen,gocritic,gocyclo
func executeRunTestsV2Step(ctx context.Context, f RunFunc, r *api.StartStepRequest, out io.Writer,
	tiConfig *tiCfg.Cfg) (*runtime.State, map[string]string, map[string]string, []byte, []*api.OutputV2, string, error) {
	start := time.Now()
	tmpFilePath := tiConfig.GetDataDir()
	fs := filesystem.New()
	log := logrus.New()
	log.Out = out
	optimizationState := types.DISABLED
	step := toStep(r)
	setTiEnvVariables(step, tiConfig)
	agentPaths := make(map[string]string)
	step.Entrypoint = r.RunTestsV2.Entrypoint
	if r.RunTestsV2.IntelligenceMode {
		links, err := instrumentation.GetV2AgentDownloadLinks(ctx, tiConfig)
		if err != nil {
			return nil, nil, nil, nil, nil, string(optimizationState), fmt.Errorf("failed to get AgentV2 URL from TI")
		}
		if len(links) < 3 {
			return nil, nil, nil, nil, nil, string(optimizationState), fmt.Errorf("error: Could not get agent V2 links from TI")
		}

		err = downloadJavaAgent(ctx, tmpFilePath, links[0].URL, fs, log)
		if err != nil {
			return nil, nil, nil, nil, nil, string(optimizationState), fmt.Errorf("failed to download Java agent")
		}

		rubyArtifactDir, err := downloadRubyAgent(ctx, tmpFilePath, links[2].URL, fs, log)
		if err != nil || rubyArtifactDir == "" {
			return nil, nil, nil, nil, nil, string(optimizationState), fmt.Errorf("failed to download Ruby agent")
		}
		agentPaths["ruby"] = rubyArtifactDir

		pythonArtifactDir, err := downloadPythonAgent(ctx, tmpFilePath, links[1].URL, fs, log)
		if err != nil {
			return nil, nil, nil, nil, nil, string(optimizationState), fmt.Errorf("failed to download Python agent")
		}
		agentPaths["python"] = pythonArtifactDir

		isPsh := IsPowershell(step.Entrypoint)
		preCmd, filterfilePath, err := getPreCmd(r.WorkingDir, tmpFilePath, fs, log, r.Envs, agentPaths, isPsh)
		if err != nil || pythonArtifactDir == "" {
			return nil, nil, nil, nil, nil, string(optimizationState), fmt.Errorf("failed to set config file or env variable to inject agent, %s", err)
		}

		commands := fmt.Sprintf("%s\n%s", preCmd, r.RunTestsV2.Command[0])
		step.Command = []string{commands}

		err = createSelectedTestFile(ctx, fs, step.Name, r.WorkingDir, log, tiConfig, tmpFilePath, r.Envs, &r.RunTestsV2, filterfilePath)
		if err != nil {
			return nil, nil, nil, nil, nil, string(optimizationState), fmt.Errorf("error while creating filter file %s", err)
		}
	} else {
		step.Command = []string{r.RunTestsV2.Command[0]}
	}
	exportEnvFile := fmt.Sprintf("%s/%s-export.env", pipeline.SharedVolPath, step.ID)
	step.Envs["DRONE_ENV"] = exportEnvFile

	if (len(r.OutputVars) > 0 || len(r.Outputs) > 0) && (len(step.Entrypoint) == 0 || len(step.Command) == 0) {
		return nil, nil, nil, nil, nil, string(optimizationState), fmt.Errorf("output variable should not be set for unset entrypoint or command")
	}

	outputFile := fmt.Sprintf("%s/%s-output.env", pipeline.SharedVolPath, step.ID)
	step.Envs["DRONE_OUTPUT"] = outputFile

	if len(r.Outputs) > 0 {
		step.Command[0] += getOutputsCmd(step.Entrypoint, r.Outputs, outputFile)
	} else if len(r.OutputVars) > 0 {
		step.Command[0] += getOutputVarCmd(step.Entrypoint, r.OutputVars, outputFile)
	}

	artifactFile := fmt.Sprintf("%s/%s-artifact", pipeline.SharedVolPath, step.ID)
	step.Envs["PLUGIN_ARTIFACT_FILE"] = artifactFile

	if metadataFile, found := step.Envs["PLUGIN_METADATA_FILE"]; found {
		step.Envs["PLUGIN_METADATA_FILE"] = fmt.Sprintf("%s/%s-%s", pipeline.SharedVolPath, step.ID, metadataFile)
	}

	exited, err := f(ctx, step, out, r.LogDrone)
	timeTakenMs := time.Since(start).Milliseconds()
	collectionErr := collectTestReportsAndCg(ctx, log, r, start, step.Name, tiConfig)
	if err == nil {
		err = collectionErr
	}

	if tiConfig.GetParseSavings() {
		optimizationState = savings.ParseAndUploadSavings(ctx, r.WorkingDir, log, step.Name, timeTakenMs, tiConfig)
	}

	useCINewGodotEnvVersion := false
	if val, ok := step.Envs[ciNewVersionGodotEnv]; ok && val == trueValue {
		useCINewGodotEnvVersion = true
	}

	exportEnvs, _ := fetchExportedVarsFromEnvFile(exportEnvFile, out, useCINewGodotEnvVersion)
	artifact, _ := fetchArtifactDataFromArtifactFile(artifactFile, out)

	if exited != nil && exited.Exited && exited.ExitCode == 0 {
		outputs, err := fetchExportedVarsFromEnvFile(outputFile, out, useCINewGodotEnvVersion) //nolint:govet
		if len(r.Outputs) > 0 {
			outputsV2 := []*api.OutputV2{}
			for _, output := range r.Outputs {
				if _, ok := outputs[output.Key]; ok {
					outputsV2 = append(outputsV2, &api.OutputV2{
						Key:   output.Key,
						Value: outputs[output.Key],
						Type:  output.Type,
					})
				}
			}
			return exited, outputs, exportEnvs, artifact, outputsV2, string(optimizationState), err
		} else if len(r.OutputVars) > 0 {
			// only return err when output vars are expected
			return exited, outputs, exportEnvs, artifact, nil, string(optimizationState), err
		}
		return exited, outputs, exportEnvs, artifact, nil, string(optimizationState), nil
	}
	return exited, nil, exportEnvs, artifact, nil, string(optimizationState), err
}

// Second parameter in return type (bool) is will be used to decide whether the filter file should be created or not.
// In case of running all the cases no filter file should be created.
func getTestsSelection(ctx context.Context, fs filesystem.FileSystem, stepID, workspace string, log *logrus.Logger,
	isManual bool, tiConfig *tiCfg.Cfg, envs map[string]string, runV2Config *api.RunTestsV2Config) (types.SelectTestsResp, bool) {
	selection := types.SelectTestsResp{}
	if isManual {
		log.Infoln("Manual execution has been detected. Running all the tests")
		return selection, false
	}
	// Question : Here i can see feature state is being defined in Runtest but here we don't have runOnlySelected tests so should we always defined as optimized state
	var files []types.File
	var err error
	runOnlySelectedTests := true

	if instrumentation.IsPushTriggerExecution(tiConfig) {
		lastSuccessfulCommitID, commitErr := instrumentation.GetCommitInfo(ctx, stepID, tiConfig)
		if commitErr != nil {
			log.Infoln("Failed to get reference commit", "error", commitErr)
			return selection, false // TI selected all the tests to be run
		}

		if lastSuccessfulCommitID != "" {
			log.Infoln("Using reference commit: ", lastSuccessfulCommitID)
			files, err = instrumentation.GetChangedFilesPush(ctx, workspace, lastSuccessfulCommitID, tiConfig.GetSha(), log)
			if err != nil {
				log.Errorln("Unable to get changed files list. Running all the tests.", "error", err)
				return selection, false
			}
		} else {
			log.Infoln("No reference commit found")
			runOnlySelectedTests = false
		}
	} else {
		files, err = instrumentation.GetChangedFilesPR(ctx, workspace, log)
		if err != nil || len(files) == 0 {
			log.Errorln("Unable to get changed files list for PR. Running all the tests.", "error", err)
			return selection, false // TI selected all the tests to be run
		}
	}
	filesWithpkg := java.ReadPkgs(log, fs, workspace, files)
	selection, err = instrumentation.SelectTests(ctx, workspace, filesWithpkg, runOnlySelectedTests, stepID, fs, tiConfig)
	if err != nil {
		log.WithError(err).Errorln("An unexpected error occurred during test selection. Running all tests.")
		runOnlySelectedTests = false
	} else if selection.SelectAll {
		log.Infoln("Test Intelligence determined to run all the tests")
		runOnlySelectedTests = false
	} else {
		log.Infoln(fmt.Sprintf("Running tests selected by Test Intelligence: %s", selection.Tests))
		runOnlySelectedTests = true
	}

	// Test splitting: only when parallelism is enabled
	if instrumentation.IsParallelismEnabled(envs) {
		runOnlySelectedTests = instrumentation.ComputeSelectedTestsV2(ctx, runV2Config, log, &selection, stepID, workspace, envs, tiConfig, runOnlySelectedTests, fs)
	}

	return selection, runOnlySelectedTests
}

func createOutDir(tmpDir string, fs filesystem.FileSystem, log *logrus.Logger) (string, error) {
	outDir := fmt.Sprintf(outDir, tmpDir)
	err := fs.MkdirAll(outDir, os.ModePerm)
	if err != nil {
		log.WithError(err).Errorln(fmt.Sprintf("could not create nested Output directory %s", outDir))
		return "", err
	}
	return outDir, nil
}

func getFilterFilePath(tmpDir string, splitIdx int) string {
	filterFileDir := fmt.Sprintf(filterV2Dir, tmpDir)

	// filterfilePath will look like /tmp/engine/ti/v2/filter/filter_1...
	filterfilePath := fmt.Sprintf("%s/filter_%d", filterFileDir, splitIdx)

	return filterfilePath
}

func createJavaConfigFile(tmpDir string, fs filesystem.FileSystem, log *logrus.Logger, filterfilePath, outDir string, splitIdx int) (string, error) {
	iniFileDir := fmt.Sprintf(configV2Dir, tmpDir)
	err := fs.MkdirAll(iniFileDir, os.ModePerm)
	if err != nil {
		log.WithError(err).Errorln(fmt.Sprintf("could not create nested directory %s", iniFileDir))
		return "", err
	}
	// create file paths with splitidx for splitting
	iniFile := fmt.Sprintf("%s/config_%d.ini", iniFileDir, splitIdx)

	data := fmt.Sprintf(`outDir: %s
	logLevel: 0
	logConsole: false
	writeTo: JSON
	packageInference: true
	filterFile: %s`, outDir, filterfilePath)

	log.Infof("Writing to %s with config:\n%s", iniFile, data)
	f, err := fs.Create(iniFile)
	if err != nil {
		log.WithError(err).Errorln(fmt.Sprintf("could not create config file %s", iniFile))
		return "", err
	}

	_, err = f.WriteString(data)
	defer f.Close()
	if err != nil {
		log.WithError(err).Errorln(fmt.Sprintf("could not write %s to config file %s", data, iniFile))
		return "", err
	}

	return iniFile, nil // path of config.ini file
}

// Here we are setting up env var to invoke agant along with creating config file and .bazelrc file
//
//nolint:funlen
func getPreCmd(workspace, tmpFilePath string, fs filesystem.FileSystem, log *logrus.Logger, envs, agentPaths map[string]string, isPsh bool) (preCmd, filterFilePath string, err error) {
	splitIdx := 0
	if instrumentation.IsParallelismEnabled(envs) {
		log.Infoln("Initializing settings for test splitting and parallelism")
		splitIdx, _ = instrumentation.GetSplitIdxAndTotal(envs)
	}

	outDir, err := createOutDir(tmpFilePath, fs, log)
	if err != nil {
		log.WithError(err).Errorln("failed to create outDir")
		return "", "", err
	}

	filterFilePath = getFilterFilePath(tmpFilePath, splitIdx)

	envs["TI"] = "1"
	envs["TI_V2"] = "1"
	envs["TI_OUTPUT_PATH"] = outDir
	envs["TI_FILTER_FILE_PATH"] = filterFilePath

	// Java
	iniFilePath, err := createJavaConfigFile(tmpFilePath, fs, log, filterFilePath, outDir, splitIdx)
	if err != nil {
		log.WithError(err).Errorln(fmt.Sprintf("could not create java agent config file in path %s", iniFilePath))
		return "", "", err
	}

	err = writetoBazelrcFile(log, fs)
	if err != nil {
		log.WithError(err).Errorln("failed to write in .bazelrc file")
		return "", "", err
	}
	javaAgentPath := fmt.Sprintf("%s%s%s", tmpFilePath, javaAgentV2Path, javaAgentV2Jar)
	agentArg := fmt.Sprintf(javaAgentV2Arg, javaAgentPath, iniFilePath)
	envs["JAVA_TOOL_OPTIONS"] = agentArg
	// Ruby
	repoPath := filepath.Join(agentPaths["ruby"], "harness", "ruby-agent")
	stepIdx, _ := instrumentation.GetStepStrategyIteration(envs)
	shouldWait := instrumentation.IsStepParallelismEnabled(envs) && stepIdx > 0
	var statusFilePath = filepath.Join(tmpFilePath, "unzip.done")
	if shouldWait {
		err = waitForFileWithTimeout(10*time.Second, statusFilePath, fs) // Wait for up to 10 seconds
		if err != nil {
			log.WithError(err).Errorln("timed out while unzipping testInfo with retry")
			return "", "", err
		}
	} else {
		repoPath, err = ruby.UnzipAndGetTestInfo(agentPaths["ruby"], log)
		if err != nil {
			log.WithError(err).Errorln("failed to unzip and get test info")
			return "", "", err
		}

		// Create a marker file indicating completion of unzip operation.
		out, err1 := fs.Create(statusFilePath)
		if err1 != nil {
			return "", "", err1
		}
		out.Close()
	}

	if !isPsh {
		preCmd = fmt.Sprintf("\nbundle add rspec_junit_formatter || true;\nbundle add harness_ruby_agent --path %q --version %q || true;", repoPath, "0.0.1")
	} else {
		preCmd = fmt.Sprintf("\ntry { bundle add rspec_junit_formatter } catch { $null };\ntry { bundle add harness_ruby_agent --path %q --version %q } catch { $null };", repoPath, "0.0.1")
	}

	disableJunitVarName := "TI_DISABLE_JUNIT_INSTRUMENTATION"
	disableJunitInstrumentation := false
	if _, ok := envs[disableJunitVarName]; ok {
		disableJunitInstrumentation = true
	}

	err = ruby.WriteRspecFile(workspace, repoPath, splitIdx, disableJunitInstrumentation)
	if err != nil {
		log.Errorln("Unable to write rspec-local file automatically", err)
		return "", "", err
	}

	// Python
	repoPath = filepath.Join(agentPaths["python"], "harness", "python-agent-v2")
	var pyStatusFilePath = filepath.Join(tmpFilePath, "pyunzip.done")
	if shouldWait {
		err = waitForFileWithTimeout(10*time.Second, pyStatusFilePath, fs) // Wait for up to 10 seconds
		if err != nil {
			log.WithError(err).Errorln("timed out while unzipping testInfo with retry")
			return "", "", err
		}
	} else {
		repoPath, err = python.UnzipAndGetTestInfoV2(agentPaths["python"], log)
		if err != nil {
			return "", "", err
		}

		// Create a marker file indicating completion of unzip operation.
		out, err1 := fs.Create(pyStatusFilePath)
		if err1 != nil {
			return "", "", err1
		}
		out.Close()
	}
	whlFilePath, err := python.FindWhlFile(repoPath)
	if err != nil {
		return "", "", err
	}

	disablePythonV2CodeModificationVarName := "TI_DISABLE_PYTHON_CODE_MODIFICATIONS"
	disablePythonV2CodeModification := false
	if _, ok := envs[disablePythonV2CodeModificationVarName]; ok {
		disablePythonV2CodeModification = true
	}

	if !isPsh {
		preCmd += fmt.Sprintf("\npython3 -m pip install %s || true;", whlFilePath)
	} else {
		preCmd += fmt.Sprintf("\ntry { python3 -m pip install %s } catch { $null };", whlFilePath)
	}

	if !disablePythonV2CodeModification {
		modifyToxFileName := filepath.Join(repoPath, "modifytox.py")
		if !isPsh {
			preCmd += fmt.Sprintf("\npython3 %s %s %s || true;", modifyToxFileName, workspace, whlFilePath)
		} else {
			preCmd += fmt.Sprintf("\ntry { python3 %s %s %s } catch { $null };", modifyToxFileName, workspace, whlFilePath)
		}
	}

	return preCmd, filterFilePath, nil
}

func downloadJavaAgent(ctx context.Context, path, javaAgentV2Url string, fs filesystem.FileSystem, log *logrus.Logger) error {
	javaAgentPath := fmt.Sprintf("%s%s", javaAgentV2Path, javaAgentV2Jar)
	dir := filepath.Join(path, javaAgentPath)
	err := instrumentation.DownloadFile(ctx, dir, javaAgentV2Url, fs)
	if err != nil {
		log.WithError(err).Errorln("could not download java agent")
		return err
	}
	return nil
}

func downloadRubyAgent(ctx context.Context, path, rubyAgentV2Url string, fs filesystem.FileSystem, log *logrus.Logger) (string, error) {
	dir := filepath.Join(path, "ruby", "ruby-agent.zip")
	installDir := filepath.Dir(dir)
	err := instrumentation.DownloadFile(ctx, dir, rubyAgentV2Url, fs)
	if err != nil {
		log.WithError(err).Errorln("could not download ruby agent")
		return "", err
	}
	return installDir, nil
}

func downloadPythonAgent(ctx context.Context, path, pythonAgentV2Url string, fs filesystem.FileSystem, log *logrus.Logger) (string, error) {
	dir := filepath.Join(path, "python", "python-agent-v2.zip")
	installDir := filepath.Dir(dir)
	err := instrumentation.DownloadFile(ctx, dir, pythonAgentV2Url, fs)
	if err != nil {
		log.WithError(err).Errorln("could not download python agent")
		return "", err
	}
	return installDir, nil
}

// This is nothing but filterfile where all the tests selected will be stored
func createSelectedTestFile(ctx context.Context, fs filesystem.FileSystem, stepID, workspace string, log *logrus.Logger,
	tiConfig *tiCfg.Cfg, tmpFilepath string, envs map[string]string, runV2Config *api.RunTestsV2Config, filterFilePath string) error {
	isManualExecution := instrumentation.IsManualExecution(tiConfig)
	resp, isFilterFilePresent := getTestsSelection(ctx, fs, stepID, workspace, log, isManualExecution, tiConfig, envs, runV2Config)

	filterFileDir := fmt.Sprintf(filterV2Dir, tmpFilepath)

	err := fs.MkdirAll(filterFileDir, os.ModePerm)
	if err != nil {
		log.WithError(err).Errorln(fmt.Sprintf("could not create nested directory %s", filterFileDir))
		return err
	}
	err = filter.PopulateItemInFilterFile(resp, filterFilePath, fs, isFilterFilePresent)

	if err != nil {
		log.WithError(err).Errorln("failed to populate items in filterfile")
		return err
	}
	return nil
}

func writetoBazelrcFile(log *logrus.Logger, fs filesystem.FileSystem) error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		log.WithError(err).Errorln("could not read home directory")
		return err
	}

	bazelrcFilePath := filepath.Join(homeDir, ".bazelrc")
	data := "test --test_env=JAVA_TOOL_OPTIONS"

	// There might be possibility of .bazelrc being already present in homeDir so checking this condition as well
	if _, err := os.Stat(bazelrcFilePath); os.IsNotExist(err) {
		f, err := fs.Create(bazelrcFilePath)
		if err != nil {
			log.WithError(err).Errorln(fmt.Sprintf("could not create file %s", bazelrcFilePath))
			return err
		}

		log.Printf(fmt.Sprintf("attempting to write %s to %s", data, bazelrcFilePath))
		_, err = f.WriteString(data)
		if err != nil {
			log.WithError(err).Errorln(fmt.Sprintf("could not write %s to file %s", data, bazelrcFilePath))
			return err
		}
	} else {
		file, err := os.OpenFile(bazelrcFilePath, os.O_APPEND|os.O_WRONLY, os.ModeAppend)
		if err != nil {
			log.WithError(err).Errorln(fmt.Sprintf("could not open the file in dir %s", bazelrcFilePath))
			return err
		}
		defer file.Close()

		log.Printf(fmt.Sprintf("attempting to write %s to %s", data, bazelrcFilePath))
		_, err = file.WriteString("\n" + data)
		if err != nil {
			log.WithError(err).Errorln(fmt.Sprintf("could not write %s to file %s", data, bazelrcFilePath))
			return err
		}
	}
	return nil
}

func collectTestReportsAndCg(ctx context.Context, log *logrus.Logger, r *api.StartStepRequest, start time.Time, stepName string, tiConfig *tiCfg.Cfg) error {
	cgStart := time.Now()

	cgErr := collectCgFn(ctx, stepName, time.Since(start).Milliseconds(), log, cgStart, tiConfig, outDir)
	if cgErr != nil {
		log.WithField("error", cgErr).Errorln(fmt.Sprintf("Unable to collect callgraph. Time taken: %s", time.Since(cgStart)))
		cgErr = fmt.Errorf("failed to collect callgraph: %s", cgErr)
	}

	if len(r.TestReport.Junit.Paths) == 0 {
		// If there are no paths specified, set Paths[0] to include all XML files
		r.TestReport.Junit.Paths = []string{"**/*.xml"}
	}

	reportStart := time.Now()
	crErr := collectTestReportsFn(ctx, r.TestReport, r.WorkingDir, stepName, log, reportStart, tiConfig, r.Envs)
	if crErr != nil {
		log.WithField("error", crErr).Errorln(fmt.Sprintf("Failed to upload report. Time taken: %s", time.Since(reportStart)))
	}
	return cgErr
}
