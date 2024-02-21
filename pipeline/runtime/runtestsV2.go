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
	"github.com/harness/lite-engine/engine"
	"github.com/harness/lite-engine/internal/filesystem"
	"github.com/harness/lite-engine/pipeline"
	"github.com/harness/lite-engine/ti/callgraph"
	tiCfg "github.com/harness/lite-engine/ti/config"
	utils "github.com/harness/lite-engine/ti/instrumentation"
	"github.com/harness/lite-engine/ti/instrumentation/java"
	"github.com/harness/lite-engine/ti/report"
	"github.com/harness/ti-client/types"
	"github.com/sirupsen/logrus"
)

const (
	outDir          = "%s/ti/new/callgraph/" // path passed as outDir in the config.ini file
	javaNewAgentArg = "-javaagent:/addon/bin/java-agent-trampoline-0.0.1-SNAPSHOT.jar=%s"
	javaAgentPath   = "/java/new"
	javaAgentUrl    = "https://raw.githubusercontent.com/ShobhitSingh11/google-api-php-client/4494215f58677113656f80d975d08027439af5a7/java-agent-trampoline-0.0.1-SNAPSHOT.jar"
	filterDir       = "ti/new/filter"
)

// Ignoring optimization state for now
func executeRunTestsV2Step(ctx context.Context, engine *engine.Engine, r *api.StartStepRequest, out io.Writer, tiConfig *tiCfg.Cfg) (*runtime.State, map[string]string, map[string]string, []byte, []*api.OutputV2, string, error) {
	start := time.Now()
	tmpFilePath := tiConfig.GetDataDir()
	fs := filesystem.New()
	log := logrus.New()
	log.Out = out
	preCmd, err := getPreCmd(tmpFilePath, fs, log)
	if err != nil {
		return nil, nil, nil, nil, nil, "", fmt.Errorf("could not set enviromnment variable to inject agent")
	}

	err = downloadJavaAgent(ctx, tmpFilePath, fs, log)
	if err != nil {
		return nil, nil, nil, nil, nil, "", fmt.Errorf("failed to download Java agent")
	}

	commands := append([]string{preCmd}, r.Run.Command...)
	step := toStep(r)
	step.Command = commands
	step.Entrypoint = r.Run.Entrypoint
	setTiEnvVariables(step, tiConfig)

	isManualExecution := utils.IsManualExecution(tiConfig)
	err = createFilterFile(ctx, fs, step.ID, r.WorkingDir, log, isManualExecution, tiConfig, tmpFilePath)
	if err != nil {
		return nil, nil, nil, nil, nil, "", fmt.Errorf("error while creating filter file %s", err)
	}
	exportEnvFile := fmt.Sprintf("%s/%s-export.env", pipeline.SharedVolPath, step.ID)
	step.Envs["DRONE_ENV"] = exportEnvFile

	if (len(r.OutputVars) > 0 || len(r.Outputs) > 0) && (len(step.Entrypoint) == 0 || len(step.Command) == 0) {
		return nil, nil, nil, nil, nil, "", fmt.Errorf("output variable should not be set for unset entrypoint or command")
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

	exited, err := engine.Run(ctx, step, out, r.LogDrone)
	collectionErr := collectTestReportsAndCg(ctx, log, r, start, step.Name, tiConfig)
	if err == nil {
		// Fail the step if run was successful but error during collection
		err = collectionErr
	}

	exportEnvs, _ := fetchExportedVarsFromEnvFile(exportEnvFile, out)
	artifact, _ := fetchArtifactDataFromArtifactFile(artifactFile, out)

	if exited != nil && exited.Exited && exited.ExitCode == 0 {
		outputs, err := fetchExportedVarsFromEnvFile(outputFile, out) //nolint:govet
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
			return exited, outputs, exportEnvs, artifact, outputsV2, "", err
		} else if len(r.OutputVars) > 0 {
			// only return err when output vars are expected
			return exited, outputs, exportEnvs, artifact, nil, "", err
		}
		return exited, outputs, exportEnvs, artifact, nil, "", nil
	}
	return exited, nil, exportEnvs, artifact, nil, "", err
}

func collectTestReportsAndCg(ctx context.Context, log *logrus.Logger, r *api.StartStepRequest, start time.Time, stepName string, tiConfig *tiCfg.Cfg) error {
	cgStart := time.Now()

	cgErr := callgraph.Upload(ctx, stepName, time.Since(start).Milliseconds(), log, cgStart, tiConfig)
	if cgErr != nil {
		log.WithField("error", cgErr).Errorln(fmt.Sprintf("Unable to collect callgraph. Time taken: %s", time.Since(cgStart)))
		cgErr = fmt.Errorf("failed to collect callgraph: %s", cgErr)
	}

	reportStart := time.Now()
	crErr := report.ParseAndUploadTests(ctx, r.TestReport, r.WorkingDir, stepName, log, reportStart, tiConfig, r.Envs)
	if crErr != nil {
		log.WithField("error", crErr).Errorln(fmt.Sprintf("Failed to upload report. Time taken: %s", time.Since(reportStart)))
	}
	return cgErr
}

func getTestsSelection(ctx context.Context, fs filesystem.FileSystem, stepID, workspace string, log *logrus.Logger,
	isManual bool, tiConfig *tiCfg.Cfg) (types.SelectTestsResp, bool) {

	selection := types.SelectTestsResp{}
	var RunOnlySelectedTests = true // Enabled true by default

	if isManual {
		log.Infoln("Manual execution has been detected. Running all the tests")
		RunOnlySelectedTests = false
		return selection, false
	}

	var files []types.File
	var err error

	if utils.IsPushTriggerExecution(tiConfig) {
		lastSuccessfulCommitID, commitErr := utils.GetCommitInfo(ctx, stepID, tiConfig)
		if commitErr != nil {
			log.Infoln("Failed to get reference commit", "error", commitErr)
			RunOnlySelectedTests = false // TI selected all the tests to be run
			return selection, false
		}

		if lastSuccessfulCommitID == "" {
			log.Infoln("Test Intelligence determined to run all the tests to bootstrap")
			RunOnlySelectedTests = false // TI selected all the tests to be run
			return selection, false
		}

		log.Infoln("Using reference commit: ", lastSuccessfulCommitID)
		files, err = utils.GetChangedFilesPush(ctx, workspace, lastSuccessfulCommitID, tiConfig.GetSha(), log)
		if err != nil {
			log.Errorln("Unable to get changed files list. Running all the tests.", "error", err)
			RunOnlySelectedTests = false
			return selection, false
		}
	} else {
		files, err = utils.GetChangedFilesPR(ctx, workspace, log)
		if err != nil || len(files) == 0 {
			log.Errorln("Unable to get changed files list for PR. Running all the tests.", "error", err)
			RunOnlySelectedTests = false
			return selection, false
		}
	}

	filesWithpkg := java.ReadPkgs(log, fs, workspace, files)
	selection, err = utils.SelectTests(ctx, workspace, filesWithpkg, RunOnlySelectedTests, stepID, fs, tiConfig)
	if err != nil {
		log.WithError(err).Errorln("There was some issue in trying to figure out tests to run. Running all the tests")
		RunOnlySelectedTests = false // run all the tests if an error was encountered
	} else {
		log.Infoln(fmt.Sprintf("Running tests selected by Test Intelligence: %s", selection.Tests))
	}

	return selection, true
}

func createJavaConfigFile(tmpDir string, fs filesystem.FileSystem, log *logrus.Logger) (string, error) {

	dir := fmt.Sprintf(outDir, tmpDir)
	err := fs.MkdirAll(dir, os.ModePerm)
	if err != nil {
		log.WithError(err).Errorln(fmt.Sprintf("could not create nested directory %s", dir))
		return "", err
	}

	iniFile := fmt.Sprintf("%s/new/config.ini", tmpDir)
	data := fmt.Sprintf(`outDir: %s
	logLevel: 2
	logConsole: false
	writeTo: COVERAGE_JSON
	packageInference: true`, dir)

	log.Infof("Attempting to write to %s with config:\n%s", iniFile, data)
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

	return iniFile, nil //path of config.ini file
}

func getPreCmd(tmpFilePath string, fs filesystem.FileSystem, log *logrus.Logger) (string, error) {
	preCmd := "set -xe\n"
	iniFilePath, err := createJavaConfigFile(tmpFilePath, fs, log)
	if err != nil {
		return "", err
	}

	err = writetoBazelrcFile(iniFilePath, log, fs)
	if err != nil {
		return "", err
	}
	agentArg := fmt.Sprintf(javaNewAgentArg, iniFilePath)
	preCmd += fmt.Sprintf("export JAVA_TOOL_OPTIONS=%s", agentArg)
	return preCmd, nil
}

func downloadJavaAgent(ctx context.Context, path string, fs filesystem.FileSystem, log *logrus.Logger) error {

	dir := filepath.Join(path, javaAgentPath)
	err := utils.DownloadFile(ctx, dir, javaAgentUrl, fs)
	if err != nil {
		return err
	}
	return nil
}

func createFilterFile(ctx context.Context, fs filesystem.FileSystem, stepID, workspace string, log *logrus.Logger,
	isManual bool, tiConfig *tiCfg.Cfg, path string) error {

	resp, isFilterFilePresent := getTestsSelection(ctx, fs, stepID, workspace, log, isManual, tiConfig)
	dir := filepath.Join(path, filterDir)
	err := populateItemInFilterFile(resp, dir, fs, isFilterFilePresent)

	if err != nil {
		return err
	}
	return nil
}

func writetoBazelrcFile(iniFilePath string, log *logrus.Logger, fs filesystem.FileSystem) error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		fmt.Println("Could not get home directory", err)
		return err
	}

	bazelrcFilePath := filepath.Join(homeDir, ".bazelrc")
	agentArg := fmt.Sprintf(javaNewAgentArg, iniFilePath)
	data := fmt.Sprintf("test --test_env JAVA_TOOL_OPTIONS=%s", agentArg)
	// if _, err := os.Stat(bazelrcFilePath); os.IsNotExist(err) {
	f, err := fs.Create(bazelrcFilePath)
	if err != nil {
		log.WithError(err).Errorln(fmt.Sprintf("could not create file %s", bazelrcFilePath))
		return err
	}
	// }

	log.Printf(fmt.Sprintf("attempting to write %s to %s", data, bazelrcFilePath))
	_, err = f.Write([]byte(data))
	if err != nil {
		log.WithError(err).Errorln(fmt.Sprintf("could not write %s to file %s", data, bazelrcFilePath))
		return err
	}

	return nil
}
