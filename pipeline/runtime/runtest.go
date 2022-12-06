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
	"github.com/harness/lite-engine/ti/instrumentation"
	"github.com/harness/lite-engine/ti/report"
	"github.com/sirupsen/logrus"
	easyFormatter "github.com/t-tomalak/logrus-easy-formatter"
)

func executeRunTestStep(ctx context.Context, engine *engine.Engine, r *api.StartStepRequest, out io.Writer) ( //nolint:gocritic
	*runtime.State, map[string]string, map[string]string, error) {
	log := &logrus.Logger{
		Out:   out,
		Level: logrus.InfoLevel,
		Formatter: &easyFormatter.Formatter{
			LogFormat: "%msg%\n",
		},
	}

	start := time.Now()
	cmd, err := instrumentation.GetCmd(ctx, &r.RunTest, r.Name, r.WorkingDir, log, r.Envs)
	if err != nil {
		return nil, nil, nil, err
	}

	step := toStep(r)
	step.Command = []string{cmd}
	step.Entrypoint = r.RunTest.Entrypoint
	setTiEnvVariables(step)

	exportEnvFile := fmt.Sprintf("%s/%s-export.env", pipeline.SharedVolPath, step.ID)
	step.Envs["DRONE_ENV"] = exportEnvFile

	if len(r.OutputVars) > 0 && len(step.Entrypoint) == 0 || len(step.Command) == 0 {
		return nil, nil, nil, fmt.Errorf("output variable should not be set for unset entrypoint or command")
	}

	outputFile := fmt.Sprintf("%s/%s.out", pipeline.SharedVolPath, step.ID)
	if len(r.OutputVars) > 0 {
		step.Command[0] += getOutputVarCmd(step.Entrypoint, r.OutputVars, outputFile)
	}

	exited, err := engine.Run(ctx, step, out)
	if rerr := report.ParseAndUploadTests(ctx, r.TestReport, r.WorkingDir, step.Name, log); rerr != nil {
		log.Errorln(fmt.Sprintf("Failed to upload report: %s", rerr))
	}

	if uerr := callgraph.Upload(ctx, step.Name, time.Since(start).Milliseconds(), log); uerr != nil {
		log.Errorln(fmt.Sprintf("Unable to collect callgraph: %s", uerr))
	}

	exportEnvs := fetchExportedEnvVars(exportEnvFile, out)
	if len(r.OutputVars) > 0 {
		if exited != nil && exited.Exited && exited.ExitCode == 0 {
			outputs, err := fetchOutputVariables(outputFile, out) //nolint:govet
			if err != nil {
				return exited, nil, exportEnvs, err
			}
			return exited, outputs, exportEnvs, err
		}
	}

	return exited, nil, exportEnvs, err
}
