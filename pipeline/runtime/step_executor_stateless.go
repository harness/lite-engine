// Copyright 2022 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package runtime

import (
	"context"
	"io"

	"github.com/harness/lite-engine/api"
	"github.com/harness/lite-engine/engine"
	"github.com/harness/lite-engine/engine/spec"
	"github.com/harness/lite-engine/errors"
	"github.com/harness/lite-engine/logstream"
	"github.com/harness/lite-engine/pipeline"

	"github.com/drone/runner-go/pipeline/runtime"
	tiCfg "github.com/harness/lite-engine/ti/config"
)

type StepExecutorStateless struct {
	stepStatus StepStatus
}

func NewStepExecutorStateless() *StepExecutorStateless {
	return &StepExecutorStateless{
		stepStatus: StepStatus{},
	}
}

func (e *StepExecutorStateless) Status() StepStatus {
	return e.stepStatus
}

func (e *StepExecutorStateless) Run(
	ctx context.Context,
	r *api.StartStepRequest,
	cfg *spec.PipelineConfig,
	writer logstream.Writer,
) (api.VMTaskExecutionResponse, error) {
	if r.ID == "" {
		return api.VMTaskExecutionResponse{}, &errors.BadRequestError{Msg: "ID needs to be set"}
	}

	e.stepStatus = StepStatus{Status: Running}

	state, outputs, envs, artifact, outputV2, optimizationState, stepErr := e.executeStep(r, cfg, writer)
	e.stepStatus = StepStatus{Status: Complete, State: state, StepErr: stepErr, Outputs: outputs, Envs: envs,
		Artifact: artifact, OutputV2: outputV2, OptimizationState: optimizationState}
	pollResponse := convertStatus(e.stepStatus)
	return convertPollResponse(pollResponse), nil
}

func (e *StepExecutorStateless) executeStep( //nolint:gocritic
	r *api.StartStepRequest,
	cfg *spec.PipelineConfig,
	writer logstream.Writer,
) (*runtime.State, map[string]string,
	map[string]string, []byte, []*api.OutputV2, string, error) {
	runFunc := func(ctx context.Context, step *spec.Step, output io.Writer, isDrone bool, isHosted bool) (*runtime.State, error) {
		return engine.RunStep(ctx, engine.Opts{}, step, output, cfg, isDrone, isHosted)
	}
	// Temporary: this should be removed once we have a better way of handling test intelligence.
	tiConfig := getTiCfg(&r.TIConfig)

	return executeStepHelper(r, runFunc, writer, &tiConfig)
}

func getTiCfg(t *api.TIConfig) tiCfg.Cfg {
	cfg := tiCfg.New(t.URL, t.Token, t.AccountID, t.OrgID, t.ProjectID, t.PipelineID, t.BuildID, t.StageID, t.Repo,
		t.Sha, t.CommitLink, t.SourceBranch, t.TargetBranch, t.CommitBranch, pipeline.SharedVolPath, t.ParseSavings, false)
	return cfg
}
