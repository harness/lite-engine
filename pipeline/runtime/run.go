// Copyright 2022 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package runtime

import (
	"context"
	"fmt"
	"io"

	"github.com/drone/runner-go/pipeline/runtime"
	"github.com/sirupsen/logrus"

	"github.com/harness/lite-engine/api"
	"github.com/harness/lite-engine/engine"
	"github.com/harness/lite-engine/pipeline"
	"github.com/harness/lite-engine/ti/report"
)

func executeRunStep(ctx context.Context, engine *engine.Engine, r *api.StartStepRequest, out io.Writer) (
	*runtime.State, map[string]string, map[string]string, error) {
	step := toStep(r)
	step.Command = r.Run.Command
	step.Entrypoint = r.Run.Entrypoint
	setTiEnvVariables(step)

	exportEnvFile := fmt.Sprintf("%s/%s-export.env", pipeline.SharedVolPath, step.ID)
	step.Envs["DRONE_ENV"] = exportEnvFile

	if len(r.OutputVars) > 0 && (len(step.Entrypoint) == 0 || len(step.Command) == 0) {
		return nil, nil, nil, fmt.Errorf("output variable should not be set for unset entrypoint or command")
	}

	outputFile := fmt.Sprintf("%s/%s.out", pipeline.SharedVolPath, step.ID)
	if len(r.OutputVars) > 0 {
		step.Command[0] += getOutputVarCmd(step.Entrypoint, r.OutputVars, outputFile)
	}

	log := logrus.New()
	log.Out = out

	exited, err := engine.Run(ctx, step, out)
	if rerr := report.ParseAndUploadTests(ctx, r.TestReport, r.WorkingDir, step.Name, log); rerr != nil {
		logrus.WithError(rerr).WithField("step", step.Name).Errorln("failed to upload report")
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
