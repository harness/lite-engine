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
	"github.com/harness/lite-engine/livelog"
	"github.com/harness/lite-engine/logstream"
	"github.com/harness/lite-engine/logstream/filestore"
	"github.com/harness/lite-engine/logstream/remote"
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

func (e *StepExecutorStateless) Run(ctx context.Context, r *api.StartStepRequest, cfg *spec.PipelineConfig) (*api.PollStepResponse, error) {
	if r.ID == "" {
		return &api.PollStepResponse{}, &errors.BadRequestError{Msg: "ID needs to be set"}
	}

	e.stepStatus = StepStatus{Status: Running}

	state, outputs, envs, artifact, outputV2, optimizationState, stepErr := e.executeStep(r, cfg)
	e.stepStatus = StepStatus{Status: Complete, State: state, StepErr: stepErr, Outputs: outputs, Envs: envs,
		Artifact: artifact, OutputV2: outputV2, OptimizationState: optimizationState}

	return convertStatus(e.stepStatus), nil
}

func getLogServiceClient(cfg api.LogConfig) logstream.Client {
	if cfg.URL != "" {
		return remote.NewHTTPClient(cfg.URL, cfg.AccountID, cfg.Token, cfg.IndirectUpload, false)
	} else {
		return filestore.New(pipeline.SharedVolPath)
	}
}

func (e *StepExecutorStateless) executeStep(r *api.StartStepRequest, cfg *spec.PipelineConfig) (*runtime.State, map[string]string, //nolint:gocritic
	map[string]string, []byte, []*api.OutputV2, string, error) {

	runFunc := func(ctx context.Context, step *spec.Step, output io.Writer, isDrone bool) (*runtime.State, error) {
		return engine.RunStep(ctx, engine.Opts{}, step, output, cfg, isDrone)
	}

	// Create a log stream for step logs
	client := getLogServiceClient(r.LogConfig)
	wc := livelog.New(client, r.LogKey, r.Name, getNudges(), false)
	wr := logstream.NewReplacer(wc, r.Secrets)
	go wr.Open() //nolint:errcheck

	tiConfig := getTiCfg(&r.TIConfig)

	return executeStepHelper(r, runFunc, wc, wr, &tiConfig)
}

func getTiCfg(t *api.TIConfig) tiCfg.Cfg {
	cfg := tiCfg.New(t.URL, t.Token, t.AccountID, t.OrgID, t.ProjectID, t.PipelineID, t.BuildID, t.StageID, t.Repo,
		t.Sha, t.CommitLink, t.SourceBranch, t.TargetBranch, t.CommitBranch, pipeline.SharedVolPath, t.ParseSavings, false)
	return cfg
}
