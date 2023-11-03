// Copyright 2022 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package runtime

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/drone/runner-go/pipeline/runtime"
	"github.com/harness/lite-engine/api"
	"github.com/harness/lite-engine/engine"
	"github.com/harness/lite-engine/pipeline"
	"github.com/harness/lite-engine/ti/callgraph"
	tiCfg "github.com/harness/lite-engine/ti/config"
	"github.com/harness/lite-engine/ti/instrumentation"
	"github.com/harness/lite-engine/ti/report"
	"github.com/sirupsen/logrus"
	easyFormatter "github.com/t-tomalak/logrus-easy-formatter"
)

var (
	collectCgFn          = callgraph.Upload
	collectTestReportsFn = report.ParseAndUploadTests
)

func executeRunTestStep(ctx context.Context, engine *engine.Engine, r *api.StartStepRequest, out io.Writer, tiConfig *tiCfg.Cfg) ( //nolint:gocritic
	*runtime.State, map[string]string, map[string]string, []byte, error) {
	log := &logrus.Logger{
		Out:   out,
		Level: logrus.InfoLevel,
		Formatter: &easyFormatter.Formatter{
			LogFormat: "%msg%\n",
		},
	}

	start := time.Now()
	cmd, err := instrumentation.GetCmd(ctx, &r.RunTest, r.Name, r.WorkingDir, log, r.Envs, tiConfig)
	instrumentation.InjectReportInformation(r)
	if err != nil {
		return nil, nil, nil, nil, err
	}

	step := toStep(r)
	step.Command = []string{cmd}
	step.Entrypoint = r.RunTest.Entrypoint
	setTiEnvVariables(step, tiConfig)

	exportEnvFile := fmt.Sprintf("%s/%s-export.env", pipeline.SharedVolPath, step.ID)
	step.Envs["DRONE_ENV"] = exportEnvFile

	if len(r.OutputVars) > 0 && len(step.Entrypoint) == 0 || len(step.Command) == 0 {
		return nil, nil, nil, nil, fmt.Errorf("output variable should not be set for unset entrypoint or command")
	}

	outputFile := fmt.Sprintf("%s/%s-output.env", pipeline.SharedVolPath, step.ID)
	if len(r.OutputVars) > 0 {
		step.Command[0] += getOutputVarCmd(step.Entrypoint, r.OutputVars, outputFile)
	}

	artifactFile := fmt.Sprintf("%s/%s-artifact", pipeline.SharedVolPath, step.ID)
	step.Envs["PLUGIN_ARTIFACT_FILE"] = artifactFile

	exited, err := engine.Run(ctx, step, out, false)
	collectionErr := collectRunTestData(ctx, log, r, start, step.Name, tiConfig)
	if err == nil {
		// Fail the step if run was successful but error during collection
		err = collectionErr
	}

	exportEnvs, _ := fetchExportedVarsFromEnvFile(exportEnvFile, out)
	artifact, _ := fetchArtifactDataFromArtifactFile(artifactFile, out)
	if len(r.OutputVars) > 0 {
		if exited != nil && exited.Exited && exited.ExitCode == 0 {
			outputs, err := fetchExportedVarsFromEnvFile(outputFile, out) //nolint:govet
			return exited, outputs, exportEnvs, artifact, err
		}
	}

	return exited, nil, exportEnvs, artifact, err
}

// collectRunTestData collects callgraph and test reports after executing the step
func collectRunTestData(ctx context.Context, log *logrus.Logger, r *api.StartStepRequest, start time.Time, stepName string, tiConfig *tiCfg.Cfg) error {
	cgStart := time.Now()
	cgErr := collectCgFn(ctx, stepName, time.Since(start).Milliseconds(), log, cgStart, tiConfig)
	if cgErr != nil {
		log.WithField("error", cgErr).Errorln(fmt.Sprintf("Unable to collect callgraph. Time taken: %s", time.Since(cgStart)))
		cgErr = fmt.Errorf("failed to collect callgraph: %s", cgErr)
	}

	reportStart := time.Now()
	crErr := collectTestReportsFn(ctx, r.TestReport, r.WorkingDir, stepName, r.RunTest.Language, log, reportStart, tiConfig)
	if crErr != nil {
		log.WithField("error", crErr).Errorln(fmt.Sprintf("Failed to upload report. Time taken: %s", time.Since(reportStart)))
	}
	return cgErr
}
