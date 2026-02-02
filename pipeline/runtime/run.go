// Copyright 2022 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/drone/runner-go/pipeline/runtime"
	"github.com/harness/lite-engine/api"
	"github.com/harness/lite-engine/common"
	"github.com/harness/lite-engine/engine"
	"github.com/harness/lite-engine/pipeline"
	tiCfg "github.com/harness/lite-engine/ti/config"
	"github.com/harness/lite-engine/ti/quarantine"
	"github.com/harness/lite-engine/ti/report"
	"github.com/harness/lite-engine/ti/savings"
	"github.com/harness/ti-client/types"
	"github.com/sirupsen/logrus"
)

func executeRunStep(ctx context.Context, f RunFunc, r *api.StartStepRequest, out io.Writer, tiConfig *tiCfg.Cfg) ( //nolint:gocritic,gocyclo,funlen
	*runtime.State, map[string]string, map[string]string, []byte, []*api.OutputV2, *types.TelemetryData, string, error) {
	start := time.Now()
	var internalTempFiles []string
	step := toStep(r)
	step.Command = r.Run.Command
	step.Entrypoint = r.Run.Entrypoint
	setTiEnvVariables(step, tiConfig)

	optimizationState := types.DISABLED
	exportEnvFile := fmt.Sprintf("%s/%s-export.env", pipeline.GetSharedVolPath(), step.ID)
	internalTempFiles = append(internalTempFiles, exportEnvFile)
	step.Envs["DRONE_ENV"] = exportEnvFile
	telemetryData := &types.TelemetryData{}

	// Set annotations file path for producers to write rich annotations JSON
	annotationsFile := fmt.Sprintf("%s/%s-annotations.json", pipeline.GetSharedVolPath(), step.ID)
	annotationsFileForEnv := engine.PathConverter(annotationsFile)
	step.Envs["HARNESS_ANNOTATIONS_FILE"] = annotationsFileForEnv

	// For Windows containers, add hcli directory to PATH
	injectHcliPathForWindowsContainer(step)

	if (len(r.OutputVars) > 0 || len(r.Outputs) > 0) && (len(step.Entrypoint) == 0 || len(step.Command) == 0) {
		return nil, nil, nil, nil, nil, nil, string(optimizationState), fmt.Errorf("output variable should not be set for unset entrypoint or command")
	}

	if r.ScratchDir != "" {
		// Plugins can use this directory as a scratch space to store temporary files.
		// It will get cleaned up after a destroy.
		internalTempFiles = append(internalTempFiles, r.ScratchDir)
		step.Envs["HARNESS_SCRATCH_DIR"] = r.ScratchDir
	}

	var outputFile string
	if r.OutputVarFile != "" {
		// If the output variable file is set, we use it to write the output variables
		outputFile = r.OutputVarFile
	} else {
		// Otherwise, we use the default output file path
		outputFile = fmt.Sprintf("%s/%s-output.env", pipeline.GetSharedVolPath(), step.ID)
	}
	internalTempFiles = append(internalTempFiles, outputFile)

	useCINewGodotEnvVersion := false
	if val, ok := step.Envs[ciNewVersionGodotEnv]; ok && val == trueValue {
		useCINewGodotEnvVersion = true
	}

	// Plugins can use HARNESS_OUTPUT_FILE to write the output variables to a file.
	step.Envs["HARNESS_OUTPUT_FILE"] = outputFile
	step.Envs["DRONE_OUTPUT"] = outputFile

	//  Here we auto append the run command to write output variables.
	if len(r.Outputs) > 0 {
		step.Command[0] += getOutputsCmd(step.Entrypoint, r.Outputs, outputFile, useCINewGodotEnvVersion)
	} else if len(r.OutputVars) > 0 {
		step.Command[0] += getOutputVarCmd(step.Entrypoint, r.OutputVars, outputFile, useCINewGodotEnvVersion)
	}

	var outputSecretsFile string
	if r.SecretVarFile != "" {
		outputSecretsFile = r.SecretVarFile
	} else {
		outputSecretsFile = fmt.Sprintf("%s/%s-output-secrets.env", pipeline.GetSharedVolPath(), step.ID)
	}
	internalTempFiles = append(internalTempFiles, outputSecretsFile)
	// Plugins can use HARNESS_OUTPUT_SECRET_FILE to write the output secrets to a file.
	step.Envs["HARNESS_OUTPUT_SECRET_FILE"] = outputSecretsFile

	artifactFile := fmt.Sprintf("%s/%s-artifact", pipeline.GetSharedVolPath(), step.ID)
	internalTempFiles = append(internalTempFiles, artifactFile)
	step.Envs["PLUGIN_ARTIFACT_FILE"] = artifactFile

	if metadataFile, found := step.Envs["PLUGIN_METADATA_FILE"]; found {
		pluginMetadataFile := fmt.Sprintf("%s/%s-%s", pipeline.GetSharedVolPath(), step.ID, metadataFile)
		step.Envs["PLUGIN_METADATA_FILE"] = pluginMetadataFile
		internalTempFiles = append(internalTempFiles, pluginMetadataFile)
	}

	if cacheMetricsFile, found := step.Envs["PLUGIN_CACHE_METRICS_FILE"]; found {
		pluginCacheMetricsFile := fmt.Sprintf("%s/%s-%s", pipeline.GetSharedVolPath(), step.ID, cacheMetricsFile)
		step.Envs["PLUGIN_CACHE_METRICS_FILE"] = pluginCacheMetricsFile
		internalTempFiles = append(internalTempFiles, pluginCacheMetricsFile)
	}

	if cacheIntelMetricsFile, found := step.Envs["PLUGIN_CACHE_INTEL_METRICS_FILE"]; found {
		pluginCacheIntelMetricsFile := fmt.Sprintf("%s/%s-%s", pipeline.GetSharedVolPath(), step.ID, cacheIntelMetricsFile)
		step.Envs["PLUGIN_CACHE_INTEL_METRICS_FILE"] = pluginCacheIntelMetricsFile
		internalTempFiles = append(internalTempFiles, pluginCacheIntelMetricsFile)
	}

	if pluginBuildToolFile, found := step.Envs["PLUGIN_BUILD_TOOL_FILE"]; found {
		pluginBuildToolFilepath := fmt.Sprintf("%s/%s-%s", pipeline.GetSharedVolPath(), step.ID, pluginBuildToolFile)
		step.Envs["PLUGIN_BUILD_TOOL_FILE"] = pluginBuildToolFilepath
		internalTempFiles = append(internalTempFiles, pluginBuildToolFilepath)
	}

	log := logrus.New()
	log.Out = out

	// stageRuntimeID is only passed for dlite
	isHosted := r.StageRuntimeID != ""

	exited, err := f(ctx, step, out, r.LogDrone, isHosted)
	timeTakenMs := time.Since(start).Milliseconds()

	reportStart := time.Now()
	tests, rerr := report.ParseAndUploadTests(ctx, r.TestReport, r.WorkingDir, step.Name, log, reportStart, tiConfig, &telemetryData.TestIntelligenceMetaData, r.Envs)
	if rerr != nil {
		logrus.WithContext(ctx).WithError(rerr).WithField("step", step.Name).Errorln("failed to upload report")
		log.Errorf("Failed to upload report. Time taken: %s", time.Since(reportStart))
	}

	// Check if all failed tests are quarantined and update exit code accordingly
	if exited != nil {
		exited.ExitCode, err = quarantine.CheckAndUpdateExitCodeForQuarantinedTests(ctx, tests, tiConfig, log, exited.ExitCode, err)
	}

	// Parse and upload savings to TI
	if tiConfig.GetParseSavings() {
		stepType := common.StepTypePlugin
		if step.Command != nil && len(step.Command) > 0 {
			stepType = common.StepTypeRun
		}
		optimizationState = savings.ParseAndUploadSavings(ctx, r.WorkingDir, log, step.Name, checkStepSuccess(exited, err), timeTakenMs, tiConfig, r.Envs, telemetryData, stepType)
	}

	// only for git-clone-step
	if buildLangFile, found := r.Envs["PLUGIN_BUILD_TOOL_FILE"]; found {
		err1 := parseBuildInfo(telemetryData, buildLangFile)
		if err1 != nil {
			logrus.WithContext(ctx).WithError(err1).Errorln("skipped parsing build info")
		}
	}

	exportEnvs, _ := fetchExportedVarsFromEnvFile(exportEnvFile, out, useCINewGodotEnvVersion)
	artifact, _ := fetchArtifactDataFromArtifactFile(artifactFile, out)
	summaryOutputs := make(map[string]string)

	if r.TestReport.Junit.Paths != nil && len(r.TestReport.Junit.Paths) > 0 {
		reportSaveErr := report.SaveReportSummaryToOutputs(ctx, tiConfig, step.Name, summaryOutputs, log, r.Envs)

		if reportSaveErr == nil && report.TestSummaryAsOutputEnabled(r.Envs) {
			log.Infof("Test summary set as output variables")
		}
	}
	summaryOutputsV2 := report.GetSummaryOutputsV2(summaryOutputs, r.Envs)

	if exited != nil && exited.Exited && exited.ExitCode == 0 {
		outputs, err := fetchExportedVarsFromEnvFile(outputFile, out, useCINewGodotEnvVersion) //nolint:govet
		if report.TestSummaryAsOutputEnabled(r.Envs) {
			if outputs == nil {
				outputs = make(map[string]string)
			}
			// add summary outputs to current outputs map
			for k, v := range summaryOutputs {
				if _, ok := outputs[k]; !ok {
					outputs[k] = v
				}
			}
		}
		outputsV2 := []*api.OutputV2{}
		var finalErr error
		if len(r.Outputs) > 0 {
			// only return err when output vars are expected
			finalErr = err
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
		} else {
			if len(r.OutputVars) > 0 {
				// only return err when output vars are expected
				finalErr = err
			}

			for key, value := range outputs {
				output := &api.OutputV2{
					Key:   key,
					Value: value,
					Type:  api.OutputTypeString,
				}
				outputsV2 = append(outputsV2, output)
			}
		}

		// checking exported secrets from plugins if any
		if _, err := os.Stat(outputSecretsFile); err == nil {
			secrets, err := fetchExportedVarsFromEnvFile(outputSecretsFile, out, useCINewGodotEnvVersion)
			if err != nil {
				log.WithError(err).Errorln("error encountered while fetching output secrets from env File")
			}
			for key, value := range secrets {
				output := &api.OutputV2{
					Key:   key,
					Value: value,
					Type:  api.OutputTypeSecret,
				}
				outputsV2 = append(outputsV2, output)
			}
		}

		if r.DeleteTempStepFiles {
			// Remove temporary internal step files
			removeFiles(internalTempFiles, log)
		}
		return exited, outputs, exportEnvs, artifact, outputsV2, telemetryData, string(optimizationState), finalErr
	}

	// Return outputs from file when step fails but output file exists
	// Presently, we do not return the output variables in case of step failures, which makes it difficult to debug CD plugins
	// in the unified stage. To solve this, we now return the output variables even in case of step failures.
	outputs, _ := fetchExportedVarsFromEnvFile(outputFile, out, useCINewGodotEnvVersion)

	outputsV2 := []*api.OutputV2{}
	if len(r.Outputs) > 0 {
		for _, output := range r.Outputs {
			if _, ok := outputs[output.Key]; ok {
				outputsV2 = append(outputsV2, &api.OutputV2{
					Key:   output.Key,
					Value: outputs[output.Key],
					Type:  output.Type,
				})
			}
		}
	} else {
		for key, value := range outputs {
			output := &api.OutputV2{
				Key:   key,
				Value: value,
				Type:  api.OutputTypeString,
			}
			outputsV2 = append(outputsV2, output)
		}
	}

	summaryOutputsV2 = report.AppendWithoutDuplicates(summaryOutputsV2, outputsV2)

	if r.DeleteTempStepFiles {
		// Remove temporary internal step files
		removeFiles(internalTempFiles, log)
	}

	// If FF is off, return only regular outputs (no summary outputs)
	if !report.TestSummaryAsOutputEnabled(r.Envs) {
		return exited, outputs, exportEnvs, artifact, outputsV2, telemetryData, string(optimizationState), err
	}

	// If FF is on but there are no outputs to return, return nil
	if len(summaryOutputsV2) == 0 {
		return exited, nil, exportEnvs, artifact, nil, telemetryData, string(optimizationState), err
	}

	// If FF is on and we have outputs, return summary outputs with regular outputs merged in
	return exited, summaryOutputs, exportEnvs, artifact, summaryOutputsV2, telemetryData, string(optimizationState), err
}

func parseBuildInfo(telemetryData *types.TelemetryData, buildFile string) error {
	if _, err := os.Stat(buildFile); os.IsNotExist(err) {
		return err
	}

	// Read the JSON file containing the cache metrics.
	data, err := os.ReadFile(buildFile)
	if err != nil {
		return err
	}

	// Deserialize the JSON data into the CacheMetrics struct.
	var buildInfo types.BuildInfo
	if err := json.Unmarshal(data, &buildInfo); err != nil {
		return err
	}

	telemetryData.BuildInfo = buildInfo
	return nil
}

// RemoveFiles attempts to delete each file or folder in the provided list.
func removeFiles(internalTempFiles []string, log *logrus.Logger) {
	for _, path := range internalTempFiles {
		// Check if file exists
		if _, err := os.Stat(path); os.IsNotExist(err) {
			// Skip if file doesn't exist
			continue
		}
		// Try to remove the file
		if err := os.RemoveAll(path); err != nil {
			log.WithError(err).WithField("path", path).Errorln("failed to remove temporary step file or folder")
		}
	}
}
