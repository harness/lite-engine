// Copyright 2022 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package runtime

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"time"

	"github.com/drone/runner-go/pipeline/runtime"
	"github.com/harness/lite-engine/api"
	"github.com/harness/lite-engine/common"
	"github.com/harness/lite-engine/internal/filesystem"
	"github.com/harness/lite-engine/pipeline"
	"github.com/harness/lite-engine/ti/callgraph"
	tiCfg "github.com/harness/lite-engine/ti/config"
	"github.com/harness/lite-engine/ti/instrumentation"
	"github.com/harness/lite-engine/ti/report"
	"github.com/harness/lite-engine/ti/savings"
	"github.com/harness/ti-client/types"
	"github.com/sirupsen/logrus"
	easyFormatter "github.com/t-tomalak/logrus-easy-formatter"
)

const (
	cgDir = "%s/ti/callgraph/" // path where callgraph files will be generated
)

var (
	collectCgFn          = callgraph.Upload
	collectTestReportsFn = report.ParseAndUploadTests
)

func executeRunTestStep(ctx context.Context, f RunFunc, r *api.StartStepRequest, out io.Writer, tiConfig *tiCfg.Cfg) ( //nolint:gocritic,gocyclo,funlen
	*runtime.State, map[string]string, map[string]string, []byte, []*api.OutputV2, *types.TelemetryData, string, error) {
	log := &logrus.Logger{
		Out:   out,
		Level: logrus.InfoLevel,
		Formatter: &easyFormatter.Formatter{
			LogFormat: "%msg%\n",
		},
	}

	start := time.Now()
	optimizationState := types.DISABLED
	telemetryData := &types.TelemetryData{}

	cmd, err := instrumentation.GetCmd(ctx, &r.RunTest, r.Name, r.WorkingDir, r.ID, log, r.Envs, tiConfig, &telemetryData.TestIntelligenceMetaData)
	if err != nil {
		return nil, nil, nil, nil, nil, nil, string(optimizationState), err
	}

	instrumentation.InjectReportInformation(r)
	step := toStep(r)
	step.Command = []string{cmd}
	step.Entrypoint = r.RunTest.Entrypoint
	setTiEnvVariables(step, tiConfig)

	exportEnvFile := fmt.Sprintf("%s/%s-export.env", pipeline.SharedVolPath, step.ID)
	step.Envs["DRONE_ENV"] = exportEnvFile

	if (len(r.OutputVars) > 0 || len(r.Outputs) > 0) && (len(step.Entrypoint) == 0 || len(step.Command) == 0) {
		return nil, nil, nil, nil, nil, nil, string(optimizationState), fmt.Errorf("output variable should not be set for unset entrypoint or command")
	}

	useCINewGodotEnvVersion := false
	if val, ok := step.Envs[ciNewVersionGodotEnv]; ok && val == trueValue {
		useCINewGodotEnvVersion = true
	}

	outputFile := fmt.Sprintf("%s/%s-output.env", pipeline.SharedVolPath, step.ID)
	if len(r.Outputs) > 0 {
		step.Command[0] += getOutputsCmd(step.Entrypoint, r.Outputs, outputFile, useCINewGodotEnvVersion)
	} else if len(r.OutputVars) > 0 {
		step.Command[0] += getOutputVarCmd(step.Entrypoint, r.OutputVars, outputFile, useCINewGodotEnvVersion)
	}

	artifactFile := fmt.Sprintf("%s/%s-artifact", pipeline.SharedVolPath, step.ID)
	step.Envs["PLUGIN_ARTIFACT_FILE"] = artifactFile

	exited, err := f(ctx, step, out, false, false)
	timeTakenMs := time.Since(start).Milliseconds()
	collectionErr := collectRunTestData(ctx, log, r, start, step.Name, tiConfig, telemetryData)
	if err == nil {
		// Fail the step if run was successful but error during collection
		err = collectionErr
	}

	// Parse and upload savings to TI
	if tiConfig.GetParseSavings() {
		optimizationState = savings.ParseAndUploadSavings(ctx, r.WorkingDir, log, step.Name, checkStepSuccess(exited, err), timeTakenMs, tiConfig, r.Envs, telemetryData, common.StepTypeRunTests)
	}

	exportEnvs, _ := fetchExportedVarsFromEnvFile(exportEnvFile, out, useCINewGodotEnvVersion)
	artifact, _ := fetchArtifactDataFromArtifactFile(artifactFile, out)

	outputs, fetchErr := fetchExportedVarsFromEnvFile(outputFile, out, useCINewGodotEnvVersion)
	if outputs == nil {
		outputs = make(map[string]string)
	}
	summaryOutputs := make(map[string]string)
	reportSaveErr := report.SaveReportSummaryToOutputs(ctx, tiConfig, step.Name, summaryOutputs, log, r.Envs)
	if reportSaveErr != nil {
		log.Warnf("Error while saving report summary to outputs %s", reportSaveErr.Error())
	}
	summaryOutputV2 := report.GetSummaryOutputsV2(summaryOutputs, r.Envs)
	if report.TestSummaryAsOutputEnabled(r.Envs) && len(summaryOutputV2) > 0 {
		// copy to outputs, we need a separate summaryOutput map to return when step fials
		for k, v := range summaryOutputs {
			if _, ok := outputs[k]; !ok {
				outputs[k] = v
			}
		}
	}

	if len(r.Outputs) > 0 {
		if exited != nil && exited.Exited && exited.ExitCode == 0 {
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
				outputsV2 = report.AppendWithoutDuplicates(outputsV2, summaryOutputV2)
			}
			// when outputvars are defined and step has succeeded, fetchErr takes priority
			return exited, outputs, exportEnvs, artifact, outputsV2, telemetryData, string(optimizationState), fetchErr
		}
		if report.TestSummaryAsOutputEnabled(r.Envs) {
			return exited, summaryOutputs, exportEnvs, artifact, summaryOutputV2, telemetryData, string(optimizationState), err
		}
	} else if len(r.OutputVars) > 0 {
		if exited != nil && exited.Exited && exited.ExitCode == 0 {
			if len(summaryOutputV2) != 0 && report.TestSummaryAsOutputEnabled(r.Envs) {
				// when step has failed return the actual error
				return exited, outputs, exportEnvs, artifact, summaryOutputV2, telemetryData, string(optimizationState), err
			}
			// when outputvars are defined and step has succeeded, fetchErr takes priority
			return exited, outputs, exportEnvs, artifact, nil, telemetryData, string(optimizationState), fetchErr
		}
		if len(outputs) != 0 && len(summaryOutputV2) != 0 && report.TestSummaryAsOutputEnabled(r.Envs) {
			// when step has failed return the actual error
			return exited, summaryOutputs, exportEnvs, artifact, summaryOutputV2, telemetryData, string(optimizationState), err
		}
	}
	if len(outputs) != 0 && len(summaryOutputV2) != 0 && report.TestSummaryAsOutputEnabled(r.Envs) {
		// when there is no output vars requested, fetchErr will have non nil value
		// In that case return err, which reflects pipeline error
		return exited, summaryOutputs, exportEnvs, artifact, summaryOutputV2, telemetryData, string(optimizationState), err
	}

	// clean up folders
	tmpFilePath := filepath.Join(tiConfig.GetDataDir(), instrumentation.GetUniqueHash(r.ID, tiConfig))
	fs := filesystem.New()
	_ = fs.Remove(tmpFilePath)

	return exited, nil, exportEnvs, artifact, nil, telemetryData, string(optimizationState), err
}

// collectRunTestData collects callgraph and test reports after executing the step
func collectRunTestData(ctx context.Context, log *logrus.Logger, r *api.StartStepRequest, start time.Time, stepName string, tiConfig *tiCfg.Cfg, telemetryData *types.TelemetryData) error {

	reportStart := time.Now()
	tests, crErr := collectTestReportsFn(ctx, r.TestReport, r.WorkingDir, stepName, log, reportStart, tiConfig, &telemetryData.TestIntelligenceMetaData, r.Envs)
	if crErr != nil {
		log.WithField("error", crErr).Errorln(fmt.Sprintf("Failed to upload report. Time taken: %s", time.Since(reportStart)))
	}

	cgStart := time.Now()
	cgErr := collectCgFn(ctx, stepName, time.Since(start).Milliseconds(), log, cgStart, tiConfig, cgDir, r.ID, false, tests)
	if cgErr != nil {
		log.WithField("error", cgErr).Errorln(fmt.Sprintf("Unable to collect callgraph. Time taken: %s", time.Since(cgStart)))
		cgErr = fmt.Errorf("failed to collect callgraph: %s", cgErr)
	}

	return cgErr
}
