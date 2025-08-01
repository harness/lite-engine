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
	goRuntime "runtime"
	"strings"
	"time"

	"github.com/drone/runner-go/pipeline/runtime"
	"github.com/harness/lite-engine/api"
	"github.com/harness/lite-engine/common"
	"github.com/harness/lite-engine/internal/filesystem"
	"github.com/harness/lite-engine/pipeline"
	tiCfg "github.com/harness/lite-engine/ti/config"
	"github.com/harness/lite-engine/ti/instrumentation"
	"github.com/harness/lite-engine/ti/instrumentation/csharp"
	"github.com/harness/lite-engine/ti/instrumentation/java"
	"github.com/harness/lite-engine/ti/instrumentation/python"
	"github.com/harness/lite-engine/ti/instrumentation/ruby"
	"github.com/harness/lite-engine/ti/report"
	"github.com/harness/lite-engine/ti/savings"
	filter "github.com/harness/lite-engine/ti/testsfilteration"
	telemetryutils "github.com/harness/ti-client/clientUtils/telemetryUtils"
	"github.com/harness/ti-client/types"
	"github.com/sirupsen/logrus"

	tiClient "github.com/harness/ti-client/client"
)

const (
	outDir                  = "%s/ti/v2/callgraph/cg/" // path passed as outDir in the config.ini file
	javaAgentV2Arg          = "-javaagent:%s=%s"
	javaAgentV2Jar          = "java-agent.jar"
	javaAgentV2Path         = "/java/v2/"
	filterV2Dir             = "%s/ti/v2/filter"
	configV2Dir             = "%s/ti/v2/java/config"
	waitTimeoutInSec        = 30
	agentV2LinkLength       = 3
	dotNetAgentLinkIndex    = 3
	dotNetAgentProfilerGUID = "{86A1D712-8FAE-4ECD-9333-DB03F62E44FA}"
	dotNetAgentV2LibLinux   = "net-agent.so"
	dotNetAgentV2LibWin     = "net-agent.dll"
	dotNetAgentV2Zip        = "dotnet-agent.zip"
	dotNetAgentV2Path       = "/dotnet/v2/"
	dotNetConfigV2Dir       = "%s/ti/v2/dotnet/config"
	javascriptRequireFile   = "ti-agent.cjs"
)

//nolint:gocritic,gocyclo,funlen
func executeRunTestsV2Step(ctx context.Context, f RunFunc, r *api.StartStepRequest, out io.Writer,
	tiConfig *tiCfg.Cfg) (*runtime.State, map[string]string, map[string]string, []byte, []*api.OutputV2, *types.TelemetryData, string, error) {
	start := time.Now()
	log := logrus.New()
	log.Out = out
	optimizationState := types.DISABLED
	step := toStep(r)
	setTiEnvVariables(step, tiConfig)
	step.Entrypoint = r.RunTestsV2.Entrypoint
	telemetryData := &types.TelemetryData{}

	preCmd, err := SetupRunTestV2(ctx, &r.RunTestsV2, step.Name, r.WorkingDir, step.ID, log, r.Envs, tiConfig, &telemetryData.TestIntelligenceMetaData)
	if err != nil {
		return nil, nil, nil, nil, nil, nil, string(optimizationState), err
	}
	command := r.RunTestsV2.Command[0]
	if preCmd != "" {
		command = fmt.Sprintf("%s\n%s", preCmd, command)
	}
	step.Command = []string{command}

	exportEnvFile := fmt.Sprintf("%s/%s-export.env", pipeline.SharedVolPath, step.ID)
	step.Envs["DRONE_ENV"] = exportEnvFile

	if (len(r.OutputVars) > 0 || len(r.Outputs) > 0) && (len(step.Entrypoint) == 0 || len(step.Command) == 0) {
		return nil, nil, nil, nil, nil, nil, string(optimizationState), fmt.Errorf("output variable should not be set for unset entrypoint or command")
	}

	outputFile := fmt.Sprintf("%s/%s-output.env", pipeline.SharedVolPath, step.ID)
	step.Envs["DRONE_OUTPUT"] = outputFile

	useCINewGodotEnvVersion := false
	if val, ok := step.Envs[ciNewVersionGodotEnv]; ok && val == trueValue {
		useCINewGodotEnvVersion = true
	}

	if len(r.Outputs) > 0 {
		step.Command[0] += getOutputsCmd(step.Entrypoint, r.Outputs, outputFile, useCINewGodotEnvVersion)
	} else if len(r.OutputVars) > 0 {
		step.Command[0] += getOutputVarCmd(step.Entrypoint, r.OutputVars, outputFile, useCINewGodotEnvVersion)
	}

	artifactFile := fmt.Sprintf("%s/%s-artifact", pipeline.SharedVolPath, step.ID)
	step.Envs["PLUGIN_ARTIFACT_FILE"] = artifactFile

	if metadataFile, found := step.Envs["PLUGIN_METADATA_FILE"]; found {
		step.Envs["PLUGIN_METADATA_FILE"] = fmt.Sprintf("%s/%s-%s", pipeline.SharedVolPath, step.ID, metadataFile)
	}

	exited, err := f(ctx, step, out, r.LogDrone, false)
	timeTakenMs := time.Since(start).Milliseconds()

	if r.RunTestsV2.IntelligenceMode {
		collectionErr := collectTestReportsAndCg(ctx, log, r, start, step.Name, tiConfig, telemetryData, r.Envs)
		if err == nil {
			err = collectionErr
		}
	} else {
		_, collectReportsErr := collectTestReports(ctx, log, r, step.Name, tiConfig, telemetryData)
		if err == nil {
			err = collectReportsErr
		}
	}

	if tiConfig.GetParseSavings() {
		optimizationState = savings.ParseAndUploadSavings(ctx, r.WorkingDir, log, step.Name, checkStepSuccess(exited, err), timeTakenMs, tiConfig, r.Envs, telemetryData, common.StepTypeRunTestsV2)
	}

	exportEnvs, _ := fetchExportedVarsFromEnvFile(exportEnvFile, out, useCINewGodotEnvVersion)
	artifact, _ := fetchArtifactDataFromArtifactFile(artifactFile, out)

	summaryOutputs := make(map[string]string)
	reportSaveErr := report.SaveReportSummaryToOutputs(ctx, tiConfig, step.Name, summaryOutputs, log, r.Envs)
	if reportSaveErr != nil {
		log.Errorf("Error while saving report summary to outputs %s", reportSaveErr.Error())
	}
	summaryOutputsV2 := report.GetSummaryOutputsV2(summaryOutputs, r.Envs)
	if exited != nil && exited.Exited && exited.ExitCode == 0 {
		outputs, err := fetchExportedVarsFromEnvFile(outputFile, out, useCINewGodotEnvVersion) //nolint:govet
		if report.TestSummaryAsOutputEnabled(r.Envs) {
			if outputs == nil {
				outputs = make(map[string]string)
			}

			for k, v := range summaryOutputs {
				if _, ok := outputs[k]; !ok {
					outputs[k] = v
				}
			}
		}
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
			if report.TestSummaryAsOutputEnabled(r.Envs) {
				outputsV2 = report.AppendWithoutDuplicates(outputsV2, summaryOutputsV2)
			}
			return exited, outputs, exportEnvs, artifact, outputsV2, telemetryData, string(optimizationState), err
		} else if len(r.OutputVars) > 0 {
			// only return err when output vars are expected
			if report.TestSummaryAsOutputEnabled(r.Envs) {
				return exited, summaryOutputs, exportEnvs, artifact, summaryOutputsV2, telemetryData, string(optimizationState), err
			}
			return exited, outputs, exportEnvs, artifact, nil, telemetryData, string(optimizationState), err
		}
		if len(summaryOutputsV2) != 0 && report.TestSummaryAsOutputEnabled(r.Envs) {
			return exited, outputs, exportEnvs, artifact, summaryOutputsV2, telemetryData, string(optimizationState), nil
		}
		return exited, outputs, exportEnvs, artifact, nil, telemetryData, string(optimizationState), nil
	}
	if len(summaryOutputsV2) != 0 && report.TestSummaryAsOutputEnabled(r.Envs) {
		return exited, summaryOutputs, exportEnvs, artifact, summaryOutputsV2, telemetryData, string(optimizationState), err
	}

	// clean up folders
	tmpFilePath := filepath.Join(tiConfig.GetDataDir(), instrumentation.GetUniqueHash(r.ID, tiConfig))
	fs := filesystem.New()
	_ = fs.Remove(tmpFilePath)

	return exited, nil, exportEnvs, artifact, nil, telemetryData, string(optimizationState), err
}

func SetupRunTestV2(
	ctx context.Context,
	config *api.RunTestsV2Config,
	stepID, workspace string,
	uniqueStepID string,
	log *logrus.Logger,
	envs map[string]string,
	tiConfig *tiCfg.Cfg,
	testMetadata *types.TestIntelligenceMetaData,
) (string, error) {
	agentPaths := make(map[string]string)
	fs := filesystem.New()
	tmpFilePath := filepath.Join(tiConfig.GetDataDir(), instrumentation.GetUniqueHash(uniqueStepID, tiConfig))

	var preCmd, filterfilePath string

	// This variable should use to pick up the qa version of the agents - this will allow a staging like option for
	// the agents, and would also help in diagnosing issues when needed. The value we look for is specific not a
	// simple "true" to have something that is more unique and hard to guess.
	qaEnvValue, ok := envs["HARNESS_TI_QA_ENV"]
	useQAEnv := ok && qaEnvValue == "QA_ENV_ENABLED"

	links, err := instrumentation.GetV2AgentDownloadLinks(ctx, tiConfig, useQAEnv)
	if err != nil {
		return preCmd, fmt.Errorf("failed to get AgentV2 URL from TI")
	}
	if len(links) < agentV2LinkLength {
		return preCmd, fmt.Errorf("error: Could not get agent V2 links from TI")
	}
	client := tiConfig.GetClient()
	err = downloadJavaAgent(ctx, tmpFilePath, links[0].URL, fs, log, client)
	if err != nil {
		return preCmd, fmt.Errorf("failed to download Java agent")
	}

	rubyArtifactDir, err := downloadRubyAgent(ctx, tmpFilePath, links[2].URL, fs, log, client)
	if err != nil || rubyArtifactDir == "" {
		return preCmd, fmt.Errorf("failed to download Ruby agent")
	}
	agentPaths["ruby"] = rubyArtifactDir

	pythonArtifactDir, err := downloadPythonAgent(ctx, tmpFilePath, links[1].URL, fs, log, client)
	if err != nil {
		return preCmd, fmt.Errorf("failed to download Python agent")
	}
	agentPaths["python"] = pythonArtifactDir

	if len(links) > dotNetAgentLinkIndex {
		var dotNetArtifactDir string
		dotNetArtifactDir, err = downloadDotNetAgent(ctx, tmpFilePath, links[dotNetAgentLinkIndex].URL, fs, log, client)
		if err == nil {
			agentPaths["dotnet"] = dotNetArtifactDir
		} else {
			log.Warningln(".net agent installation failed. Continuing without .net support.")
		}
	}
	isPsh := IsPowershell(config.Entrypoint)
	preCmd, filterfilePath, err = getPreCmd(workspace, tmpFilePath, fs, log, envs, agentPaths, isPsh, tiConfig)
	if err != nil || pythonArtifactDir == "" {
		return preCmd, fmt.Errorf("failed to set config file or env variable to inject agent, %s", err)
	}
	err = createSelectedTestFile(ctx, fs, stepID, workspace, log, tiConfig, tmpFilePath, envs, config, filterfilePath, testMetadata)
	if err != nil {
		return preCmd, fmt.Errorf("error while creating filter file %s", err)
	}

	return preCmd, nil
}

// Second parameter in return type (bool) is will be used to decide whether the filter file should be created or not.
// In case of running all the cases no filter file should be created.
func getTestsSelection(ctx context.Context, fs filesystem.FileSystem, stepID, workspace string, log *logrus.Logger,
	isManual bool, tiConfig *tiCfg.Cfg, envs map[string]string, runV2Config *api.RunTestsV2Config) (types.SelectTestsResp, bool) {
	selection := types.SelectTestsResp{}
	runOnlySelectedTests := false
	testGlobs := sanitizeTestGlobsV2(runV2Config.TestGlobs)

	if runV2Config.IntelligenceMode {
		if isManual {
			log.Infoln("Manual execution has been detected. Running all the tests")
			runOnlySelectedTests = false
		} else {
			selection, runOnlySelectedTests = getTestsSelectionWithTiModeEnabled(ctx, fs, stepID, workspace, log, isManual, tiConfig, envs, runV2Config, testGlobs)
		}
	}

	// Test splitting: only when parallelism is enabled
	if instrumentation.IsParallelismEnabled(envs) {
		log.Debugln("Parallelism is enabled!")
		runOnlySelectedTests = instrumentation.ComputeSelectedTestsV2(ctx, runV2Config, log, &selection, stepID, workspace, envs, testGlobs, tiConfig, runOnlySelectedTests, fs)
	}

	return selection, runOnlySelectedTests
}

func getTestsSelectionWithTiModeEnabled(ctx context.Context, fs filesystem.FileSystem, stepID, workspace string, log *logrus.Logger,
	isManual bool, tiConfig *tiCfg.Cfg, envs map[string]string, runV2Config *api.RunTestsV2Config, testGlobs []string) (types.SelectTestsResp, bool) { //nolint:unparam
	selection := types.SelectTestsResp{}
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
	selection, err = instrumentation.SelectTests(ctx, workspace, filesWithpkg, runOnlySelectedTests, stepID, testGlobs, fs, tiConfig)
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

func createDotNetConfigFile(tmpDir string, fs filesystem.FileSystem, log *logrus.Logger, filterfilePath, outDir string, splitIdx int) (string, error) {
	jsonFileDir := fmt.Sprintf(dotNetConfigV2Dir, tmpDir)
	err := fs.MkdirAll(jsonFileDir, os.ModePerm)
	if err != nil {
		log.WithError(err).Errorln(fmt.Sprintf("could not create nested directory %s", jsonFileDir))
		return "", err
	}
	// create file paths with splitidx for splitting
	jsonFile := fmt.Sprintf("%s/config_%d.json", jsonFileDir, splitIdx)

	data := fmt.Sprintf(`{
		"logging":{
			"level": "information",
			"console": "false",
			"file": "%s/log"
		},
		"outdir": "%s",
		"filterFile": "%s"
	}`, outDir, outDir, filterfilePath)

	log.Infof("Writing to %s with config:\n%s", jsonFile, data)
	f, err := fs.Create(jsonFile)
	if err != nil {
		log.WithError(err).Errorln(fmt.Sprintf("could not create config file %s", jsonFile))
		return "", err
	}

	_, err = f.WriteString(data)
	defer f.Close()
	if err != nil {
		log.WithError(err).Errorln(fmt.Sprintf("could not write %s to config file %s", data, jsonFile))
		return "", err
	}

	return jsonFile, nil // path of config.json file
}

// Here we are setting up env var to invoke agant along with creating config file and .bazelrc file
//
//nolint:funlen,gocyclo,lll,unparam
func getPreCmd(workspace, tmpFilePath string, fs filesystem.FileSystem, log *logrus.Logger, envs, agentPaths map[string]string, isPsh bool, tiConfig *tiCfg.Cfg) (preCmd, filterFilePath string, err error) {
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
	envs["PYTEST_ADDOPTS"] = "--cov=. --cov-report=xml"
	envs["COVERAGE_FILE"] = fmt.Sprintf(".harnesscoverage_%d", splitIdx)

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

	repoPath, err := ruby.UnzipAndGetTestInfo(agentPaths["ruby"], log)
	if err != nil {
		log.WithError(err).Errorln("failed to unzip and get test info for the ruby agent")
		return "", "", err
	}

	repoPathPython, err := python.UnzipAndGetTestInfoV2(agentPaths["python"], log)
	if err != nil {
		return "", "", err
	}

	if agentPath, exists := agentPaths["dotnet"]; exists {
		err = csharp.Unzip(agentPath, log)
		if err != nil {
			return "", "", err
		}
	}

	// Use DEBUG to redirect error logs as needed
	if !isPsh {
		preCmd = fmt.Sprintf(`if [ "$(printf %%s "$DEBUG" | tr '[:upper:]' '[:lower:]')" != "true" ]; then redir="2>/dev/null"; else redir=""; fi;`)
	}

	if !isPsh {
		preCmd += fmt.Sprintf(`
if command -v bundle >/dev/null; then
  if [ "$redir" = "2>/dev/null" ]; then
    { bundle add rspec_junit_formatter 2>/dev/null && \
      bundle add harness_ruby_agent --path "%s" --version "%s" 2>/dev/null; } \
      || echo 'Error: Failed to add rspec_junit_formatter, harness_ruby_agent.'
  else
    { bundle add rspec_junit_formatter && \
      bundle add harness_ruby_agent --path "%s" --version "%s"; } \
      || echo 'Error: Failed to add rspec_junit_formatter, harness_ruby_agent.'
  fi
fi
`, repoPath, "0.0.1", repoPath, "0.0.1")
	} else {
		preCmd += fmt.Sprintf(`\nif ($env:DEBUG -ieq 'true') { bundle add rspec_junit_formatter; bundle add harness_ruby_agent --path '%s' --version '%s' } else { try { bundle add rspec_junit_formatter 2>$null } catch { Write-Host 'Error: Failed to add rspec_junit_formatter.' }; try { bundle add harness_ruby_agent --path '%s' --version '%s' 2>$null } catch { Write-Host 'Error: Failed to add harness_ruby_agent.' } };`, repoPath, "0.0.1", repoPath, "0.0.1")
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
	whlFilePath, err := python.FindWhlFile(repoPathPython)
	if err != nil {
		return "", "", err
	}

	pyPluginFilePath, _ := python.FindPyPluginFile(repoPathPython)

	disablePythonV2CodeModification := false
	if _, ok := envs["TI_DISABLE_PYTHON_CODE_MODIFICATIONS"]; ok {
		disablePythonV2CodeModification = true
	}

	if pyPluginFilePath != "" {
		// .py plugin present — skip wheel install
		log.Infof("Found .py plugin file. Setting PYTEST_PLUGINS and PYTHONPATH.")
		envs["PYTEST_PLUGINS"] = "harness_ti_pytest_plugin"
		envs["PYTHONPATH"] = filepath.Dir(pyPluginFilePath)

	} else {
		// .py plugin file not found — fall back to .whl install
		log.Warnln("No .py plugin file found. Falling back to .whl install.")

		if !isPsh {
			preCmd += fmt.Sprintf(`
if command -v python3 >/dev/null; then
  if [ "$redir" = "2>/dev/null" ]; then
    python3 -m pip install %s 2>/dev/null || echo 'Error: Failed to install Python agent.'
  else
    python3 -m pip install %s || echo 'Error: Failed to install Python agent.'
  fi
fi
`, whlFilePath, whlFilePath)
		} else {
			preCmd += fmt.Sprintf(`\nif ($env:DEBUG -ieq 'true') { python3 -m pip install %s } else { try { python3 -m pip install %s 2>$null } catch { Write-Host 'Error: Failed to install Python agent.' } };`, whlFilePath, whlFilePath)
		}
	}

	// Run modifytox.py only if not disabled
	if !disablePythonV2CodeModification {
		modifyToxFileName := filepath.Join(repoPathPython, "modifytox.py")
		if !isPsh {
			preCmd += fmt.Sprintf("\npython3 %s %s %s || true;", modifyToxFileName, workspace, whlFilePath)
		} else {
			preCmd += fmt.Sprintf("\ntry { python3 %s %s %s } catch { $null };", modifyToxFileName, workspace, whlFilePath)
		}
	}

	// .Net
	if _, exists := agentPaths["dotnet"]; exists {
		dotNetJSONFilePath, err := createDotNetConfigFile(tmpFilePath, fs, log, filterFilePath, outDir, splitIdx)
		if err != nil {
			log.WithError(err).Errorln(fmt.Sprintf("could not create dotnet agent config file in path %s", dotNetJSONFilePath))
			return "", "", err
		}

		dotNetAgentPath := fmt.Sprintf("%s%s%s", tmpFilePath, dotNetAgentV2Path, dotNetAgentV2LibLinux)
		envs["CORECLR_PROFILER_PATH"] = dotNetAgentPath

		if goRuntime.GOOS == "linux" {
			dotNetAgentPathLinux := fmt.Sprintf("%s%slinux/%s", tmpFilePath, dotNetAgentV2Path, dotNetAgentV2LibLinux)
			dotNetAgentPathAlpine := fmt.Sprintf("%s%salpine/%s", tmpFilePath, dotNetAgentV2Path, dotNetAgentV2LibLinux)

			envs["CORECLR_PROFILER_PATH_ALPINE"] = dotNetAgentPathAlpine
			envs["CORECLR_PROFILER_PATH_LINUX"] = dotNetAgentPathLinux
			envs["CORECLR_PROFILER_PATH"] = dotNetAgentPathLinux

			if !isPsh {
				preCmd += "\nif cat /etc/os-release | grep -iq alpine ; then export CORECLR_PROFILER_PATH=$CORECLR_PROFILER_PATH_ALPINE; fi;"
			} else {
				preCmd += "\nIf (Get-Content /etc/os-release | %{$_ -match 'alpine'}) { [System.Environment]::SetEnvironmentVariable('CORECLR_PROFILER_PATH', [System.Environment]::GetEnvironmentVariable('CORECLR_PROFILER_PATH_ALPINE')); }"
			}

			if jsFFVal, ok := envs["CI_ENABLE_RUNTESTV2_JS_FF"]; ok && jsFFVal == trueValue {
				jsAgentPathLinux := fmt.Sprintf("%s%slinux/%s", tmpFilePath, dotNetAgentV2Path, javascriptRequireFile)
				jsAgentPathAlpine := fmt.Sprintf("%s%salpine/%s", tmpFilePath, dotNetAgentV2Path, javascriptRequireFile)

				envs["NODE_OPTIONS"] = fmt.Sprintf("-r %s", jsAgentPathLinux)
				envs["NODE_OPTIONS_ALPINE"] = fmt.Sprintf("-r %s", jsAgentPathAlpine)
				envs["NODE_OPTIONS_LINUX"] = fmt.Sprintf("-r %s", jsAgentPathLinux)

				if !isPsh {
					preCmd += "\nif cat /etc/os-release | grep -iq alpine ; then export NODE_OPTIONS=$NODE_OPTIONS_ALPINE; fi;"
				} else {
					preCmd += "\nIf (Get-Content /etc/os-release | %{$_ -match 'alpine'}) { [System.Environment]::SetEnvironmentVariable('NODE_OPTIONS', [System.Environment]::GetEnvironmentVariable('NODE_OPTIONS_ALPINE')); }"
				}
			}
		}

		if goRuntime.GOOS == "windows" {
			dotNetAgentPathWindows := fmt.Sprintf("%s%spack/%s", tmpFilePath, dotNetAgentV2Path, dotNetAgentV2LibWin)
			envs["CORECLR_PROFILER_PATH"] = dotNetAgentPathWindows
			if jsFFVal, ok := envs["CI_ENABLE_RUNTESTV2_JS_FF"]; ok && jsFFVal == trueValue {
				jsAgentPathWindows := fmt.Sprintf("%s%spack/%s", tmpFilePath, dotNetAgentV2Path, javascriptRequireFile)
				envs["NODE_OPTIONS"] = fmt.Sprintf("-r %s", jsAgentPathWindows)
			}
		}
		envs["CORECLR_PROFILER"] = dotNetAgentProfilerGUID
		envs["CORECLR_ENABLE_PROFILING"] = "1"
		envs["TI_DOTNET_CONFIG"] = dotNetJSONFilePath
	}
	return preCmd, filterFilePath, nil
}

func downloadJavaAgent(ctx context.Context, path, javaAgentV2Url string, fs filesystem.FileSystem, log *logrus.Logger, client tiClient.Client) error {
	javaAgentPath := fmt.Sprintf("%s%s", javaAgentV2Path, javaAgentV2Jar)
	dir := filepath.Join(path, javaAgentPath)
	err := instrumentation.DownloadFile(ctx, dir, javaAgentV2Url, fs, client)
	if err != nil {
		log.WithError(err).Errorln("could not download java agent")
		return err
	}
	return nil
}

func downloadRubyAgent(ctx context.Context, path, rubyAgentV2Url string, fs filesystem.FileSystem, log *logrus.Logger, client tiClient.Client) (string, error) {
	dir := filepath.Join(path, "ruby", "ruby-agent.zip")
	installDir := filepath.Dir(dir)
	err := instrumentation.DownloadFile(ctx, dir, rubyAgentV2Url, fs, client)
	if err != nil {
		log.WithError(err).Errorln("could not download ruby agent")
		return "", err
	}
	return installDir, nil
}

func downloadPythonAgent(ctx context.Context, path, pythonAgentV2Url string, fs filesystem.FileSystem, log *logrus.Logger, client tiClient.Client) (string, error) {
	dir := filepath.Join(path, "python", "python-agent-v2.zip")
	installDir := filepath.Dir(dir)
	err := instrumentation.DownloadFile(ctx, dir, pythonAgentV2Url, fs, client)
	if err != nil {
		log.WithError(err).Errorln("could not download python agent")
		return "", err
	}
	return installDir, nil
}

func downloadDotNetAgent(ctx context.Context, path, dotNetAgentV2Url string, fs filesystem.FileSystem, log *logrus.Logger, client tiClient.Client) (string, error) {
	dotNetAgentPath := fmt.Sprintf("%s%s", dotNetAgentV2Path, dotNetAgentV2Zip)
	dir := filepath.Join(path, dotNetAgentPath)
	installDir := filepath.Dir(dir)
	err := instrumentation.DownloadFile(ctx, dir, dotNetAgentV2Url, fs, client)
	if err != nil {
		log.WithError(err).Errorln("could not download .net agent")
		return "", err
	}
	return installDir, nil
}

// This is nothing but filterfile where all the tests selected will be stored
func createSelectedTestFile(ctx context.Context, fs filesystem.FileSystem, stepID, workspace string, log *logrus.Logger,
	tiConfig *tiCfg.Cfg, tmpFilepath string, envs map[string]string, runV2Config *api.RunTestsV2Config, filterFilePath string, testMetadata *types.TestIntelligenceMetaData) error {
	isManualExecution := instrumentation.IsManualExecution(tiConfig)
	resp, isFilterFilePresent := getTestsSelection(ctx, fs, stepID, workspace, log, isManualExecution, tiConfig, envs, runV2Config)
	if runV2Config.IntelligenceMode {
		if tiConfig.GetParseSavings() {
			if isFilterFilePresent {
				// TI selected subset of tests
				tiConfig.WriteFeatureState(stepID, types.TI, types.OPTIMIZED)
			} else {
				// TI selected all tests or returned an error which resulted in full run
				tiConfig.WriteFeatureState(stepID, types.TI, types.FULL_RUN)
			}
		}
	}

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
	testMetadata.TotalSelectedTests = resp.SelectedTests
	testMetadata.TotalSelectedTestClass = telemetryutils.CountDistinctSelectedClasses(resp.Tests)
	testMetadata.IsRunTestV2 = true
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

func collectTestReports(
	ctx context.Context,
	log *logrus.Logger,
	r *api.StartStepRequest,
	stepName string,
	tiConfig *tiCfg.Cfg,
	telemetryData *types.TelemetryData,
) ([]*types.TestCase, error) {
	if len(r.TestReport.Junit.Paths) == 0 {
		// If there are no paths specified, set Paths[0] to include all XML files and all TRX files
		r.TestReport.Junit.Paths = []string{"**/*.xml", "**/*.trx"}
	}

	reportStart := time.Now()
	tests, crErr := collectTestReportsFn(ctx, r.TestReport, r.WorkingDir, stepName, log, reportStart, tiConfig, &telemetryData.TestIntelligenceMetaData, r.Envs)
	if crErr != nil {
		log.WithField("error", crErr).Errorln(fmt.Sprintf("Failed to upload report. Time taken: %s", time.Since(reportStart)))
	}

	return tests, crErr
}

func collectTestReportsAndCg(
	ctx context.Context,
	log *logrus.Logger,
	r *api.StartStepRequest,
	start time.Time,
	stepName string,
	tiConfig *tiCfg.Cfg,
	telemetryData *types.TelemetryData,
	envs map[string]string,
) error {
	cgStart := time.Now()

	tests, _ := collectTestReports(ctx, log, r, stepName, tiConfig, telemetryData)

	testFailed := false

	if envValue, ok := envs["DISABLE_CG_UPLOAD_ON_FAILURE_FF"]; ok {
		if envValue == "true" && tests != nil {
			for _, test := range tests {
				if test.Result.Status == types.StatusFailed {
					testFailed = true
					break
				}
			}
		}
	}

	cgErr := collectCgFn(ctx, stepName, time.Since(start).Milliseconds(), log, cgStart, tiConfig, outDir, r.ID, testFailed)
	if cgErr != nil {
		log.WithField("error", cgErr).Errorln(fmt.Sprintf("Unable to collect callgraph. Time taken: %s", time.Since(cgStart)))
		cgErr = fmt.Errorf("failed to collect callgraph: %s", cgErr)
	}
	return cgErr
}

func sanitizeTestGlobsV2(globStrings []string) []string {
	var result = make([]string, 0)
	for _, globString := range globStrings {
		if globString != "" {
			splitted := strings.Split(globString, ",")
			for _, s := range splitted {
				if s != "" {
					result = append(result, s)
				}
			}
		}
	}
	return result
}
