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
	tiCfg "github.com/harness/lite-engine/ti/config"
	utils "github.com/harness/lite-engine/ti/instrumentation"
	"github.com/harness/lite-engine/ti/instrumentation/java"
	"github.com/harness/lite-engine/ti/savings"
	filter "github.com/harness/lite-engine/ti/testsfilteration"
	"github.com/harness/ti-client/types"
	"github.com/sirupsen/logrus"
)

const (
	outDir          = "%s/ti/v2/callgraph/" // path passed as outDir in the config.ini file
	javaAgentV2Arg  = "-javaagent:%s=%s"
	javaAgentV2Jar  = "java-agent-trampoline-0.0.1-SNAPSHOT.jar"
	javaAgentV2Path = "/java/v2/"
	javaAgentV2Url  = "https://raw.githubusercontent.com/ShobhitSingh11/google-api-php-client/4494215f58677113656f80d975d08027439af5a7/java-agent-trampoline-0.0.1-SNAPSHOT.jar" //May be changed later
	filterDir       = "ti/v2/callgraph"
)

// Ignoring optimization state for now
func executeRunTestsV2Step(ctx context.Context, engine *engine.Engine, r *api.StartStepRequest, out io.Writer, tiConfig *tiCfg.Cfg) (*runtime.State, map[string]string, map[string]string, []byte, []*api.OutputV2, string, error) {
	start := time.Now()
	tmpFilePath := tiConfig.GetDataDir()
	fs := filesystem.New()
	log := logrus.New()
	log.Out = out
	optimizationState := types.DISABLED
	preCmd, err := getPreCmd(tmpFilePath, fs, log)
	if err != nil {
		return nil, nil, nil, nil, nil, string(optimizationState), fmt.Errorf("failed to set config file or env variable to inject agent")
	}

	err = downloadJavaAgent(ctx, tmpFilePath, fs, log)
	if err != nil {
		return nil, nil, nil, nil, nil, string(optimizationState), fmt.Errorf("failed to download Java agent")
	}

	commands := fmt.Sprintf("%s\n%s", preCmd, r.RunTestsV2.Command[0])
	step := toStep(r)
	step.Command = []string{commands}
	step.Entrypoint = r.RunTestsV2.Entrypoint
	setTiEnvVariables(step, tiConfig)
	err = createSelectedTestFile(ctx, fs, step.ID, r.WorkingDir, log, tiConfig, tmpFilePath)
	if err != nil {
		return nil, nil, nil, nil, nil, string(optimizationState), fmt.Errorf("error while creating filter file %s", err)
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

	exited, err := engine.Run(ctx, step, out, r.LogDrone)
	timeTakenMs := time.Since(start).Milliseconds()
	collectionErr := CollectRunTestData(ctx, log, r, start, step.Name, tiConfig)
	if err == nil {
		err = collectionErr
	}

	if tiConfig.GetParseSavings() {
		optimizationState = savings.ParseAndUploadSavings(ctx, r.WorkingDir, log, step.Name, timeTakenMs, tiConfig)
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
	isManual bool, tiConfig *tiCfg.Cfg) (types.SelectTestsResp, bool) {

	selection := types.SelectTestsResp{}

	if isManual {
		log.Infoln("Manual execution has been detected. Running all the tests")
		return selection, false
	}

	// Question : Here i can see feature state is being defined in Runtest but here we don't have runOnlySelected tests so should we always defined as optimized state
	var files []types.File
	var err error

	if utils.IsPushTriggerExecution(tiConfig) {
		lastSuccessfulCommitID, commitErr := utils.GetCommitInfo(ctx, stepID, tiConfig)
		if commitErr != nil {
			log.Infoln("Failed to get reference commit", "error", commitErr)
			return selection, false // TI selected all the tests to be run
		}

		if lastSuccessfulCommitID == "" {
			log.Infoln("Test Intelligence determined to run all the tests to bootstrap")
			return selection, false // TI selected all the tests to be run
		}

		log.Infoln("Using reference commit: ", lastSuccessfulCommitID)
		files, err = utils.GetChangedFilesPush(ctx, workspace, lastSuccessfulCommitID, tiConfig.GetSha(), log)
		if err != nil {
			log.Errorln("Unable to get changed files list. Running all the tests.", "error", err)
			return selection, false // TI selected all the tests to be run
		}
	} else {
		files, err = utils.GetChangedFilesPR(ctx, workspace, log)
		if err != nil || len(files) == 0 {
			log.Errorln("Unable to get changed files list for PR. Running all the tests.", "error", err)
			return selection, false // TI selected all the tests to be run
		}
	}

	filesWithpkg := java.ReadPkgs(log, fs, workspace, files)
	selection, err = utils.SelectTests(ctx, workspace, filesWithpkg, true, stepID, fs, tiConfig)
	if err != nil {
		log.WithError(err).Errorln("An unexpected error occurred during test selection. Running all tests.")
		return selection, false
	} else if selection.SelectAll {
		log.Infoln("Test Intelligence determined to run all the tests")
		return selection, false
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

	iniFileDir := fmt.Sprintf("%s/new", tmpDir)
	err = fs.MkdirAll(iniFileDir, os.ModePerm)
	if err != nil {
		log.WithError(err).Errorln(fmt.Sprintf("could not create nested directory %s", iniFileDir))
		return "", err
	}
	iniFile := fmt.Sprintf("%s/config.ini", iniFileDir)
	data := fmt.Sprintf(`outDir: %s
	logLevel: 0 
	logConsole: false
	writeTo: JSON
	packageInference: true`, dir)

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

	return iniFile, nil //path of config.ini file
}

// Here we are setting up env var to invoke agant along with creating config file and .bazelrc file
func getPreCmd(tmpFilePath string, fs filesystem.FileSystem, log *logrus.Logger) (string, error) {
	var preCmd string
	iniFilePath, err := createJavaConfigFile(tmpFilePath, fs, log)
	if err != nil {
		log.WithError(err).Errorln(fmt.Sprintf("could not create java agent config file in path %s", iniFilePath))
		return "", err
	}

	err = writetoBazelrcFile(iniFilePath, log, fs, tmpFilePath)
	if err != nil {
		log.WithError(err).Errorln(fmt.Sprintf("failed to write in .bazelrc file"))
		return "", err
	}
	javaAgentPath := fmt.Sprintf("%s%s%s", tmpFilePath, javaAgentV2Path, javaAgentV2Jar)
	agentArg := fmt.Sprintf(javaAgentV2Arg, javaAgentPath, iniFilePath)
	preCmd = fmt.Sprintf("export JAVA_TOOL_OPTIONS=%s", agentArg)
	return preCmd, nil
}

func downloadJavaAgent(ctx context.Context, path string, fs filesystem.FileSystem, log *logrus.Logger) error {

	javaAgentPath := fmt.Sprintf("%s%s", javaAgentV2Path, javaAgentV2Jar)
	dir := filepath.Join(path, javaAgentPath)
	err := utils.DownloadFile(ctx, dir, javaAgentV2Url, fs)
	if err != nil {
		log.WithError(err).Errorln(fmt.Sprintf("could not download java agent"))
		return err
	}
	return nil
}

// This is nothing but filterfile where all the tests selected will be stored
func createSelectedTestFile(ctx context.Context, fs filesystem.FileSystem, stepID, workspace string, log *logrus.Logger,
	tiConfig *tiCfg.Cfg, path string) error {

	isManualExecution := utils.IsManualExecution(tiConfig)
	resp, isFilterFilePresent := getTestsSelection(ctx, fs, stepID, workspace, log, isManualExecution, tiConfig)
	dir := filepath.Join(path, filterDir)
	err := fs.MkdirAll(dir, os.ModePerm)
	if err != nil {
		log.WithError(err).Errorln(fmt.Sprintf("could not create nested directory %s", dir))
		return err
	}
	err = filter.PopulateItemInFilterFile(resp, dir, fs, isFilterFilePresent)

	if err != nil {
		return err
	}
	return nil
}

func writetoBazelrcFile(iniFilePath string, log *logrus.Logger, fs filesystem.FileSystem, tmpFilePath string) error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		fmt.Println("Could not get home directory", err)
		return err
	}

	javaAgentPath := fmt.Sprintf("%s%s%s", tmpFilePath, javaAgentV2Path, javaAgentV2Jar)
	agentArg := fmt.Sprintf(javaAgentV2Arg, javaAgentPath, iniFilePath)
	bazelrcFilePath := filepath.Join(homeDir, ".bazelrc")
	data := fmt.Sprintf("test --test_env JAVA_TOOL_OPTIONS=%s", agentArg)

	// There might be possibility of .bazelrc being already present in homeDir so checking this condition as well
	if _, err := os.Stat(bazelrcFilePath); os.IsNotExist(err) {
		f, err := fs.Create(bazelrcFilePath)
		if err != nil {
			log.WithError(err).Errorln(fmt.Sprintf("could not create file %s", bazelrcFilePath))
			return err
		}

		log.Printf(fmt.Sprintf("attempting to write %s to %s", data, bazelrcFilePath))
		_, err = f.Write([]byte(data))
		if err != nil {
			log.WithError(err).Errorln(fmt.Sprintf("could not write %s to file %s", data, bazelrcFilePath))
			return err
		}
	} else {
		file, err := os.OpenFile(bazelrcFilePath, os.O_APPEND|os.O_WRONLY, 0644)
		if err != nil {
			log.WithError(err).Errorln(fmt.Sprintf("could not open the file in dir %s", bazelrcFilePath))
			return err
		}
		defer file.Close()

		log.Printf(fmt.Sprintf("attempting to write %s to %s", data, bazelrcFilePath))
		_, err = file.WriteString(data)
		if err != nil {
			log.WithError(err).Errorln(fmt.Sprintf("could not write %s to file %s", data, bazelrcFilePath))
			return err
		}
	}
	return nil
}
