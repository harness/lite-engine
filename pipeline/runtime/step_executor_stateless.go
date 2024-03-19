// Copyright 2022 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package runtime

import (
	"context"
	"fmt"
	"io"
	"sync"
	"time"

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

	"github.com/hashicorp/go-multierror"
	"github.com/sirupsen/logrus"
)

type RunFunc func(ctx context.Context, step *spec.Step, output io.Writer, isDrone bool) (*runtime.State, error)

type StepExecutorStateless struct {
	mu         sync.Mutex
	stepStatus StepStatus
}

func NewStepExecutorStateless() *StepExecutorStateless {
	return &StepExecutorStateless{
		mu:         sync.Mutex{},
		stepStatus: StepStatus{},
	}
}

func (e *StepExecutorStateless) Status() StepStatus {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.stepStatus
}

func (e *StepExecutorStateless) Run(ctx context.Context, r *api.StartStepRequest, cfg *spec.PipelineConfig) (*api.PollStepResponse, error) {
	if r.ID == "" {
		return &api.PollStepResponse{}, &errors.BadRequestError{Msg: "ID needs to be set"}
	}

	e.mu.Lock()
	e.stepStatus = StepStatus{Status: Running}
	e.mu.Unlock()

	state, outputs, envs, artifact, outputV2, optimizationState, stepErr := e.executeStep(r, cfg)
	status := StepStatus{Status: Complete, State: state, StepErr: stepErr, Outputs: outputs, Envs: envs,
		Artifact: artifact, OutputV2: outputV2, OptimizationState: optimizationState}
	e.mu.Lock()
	e.stepStatus = status
	e.mu.Unlock()

	return convertStatus(status), nil
}

func getLogServiceClient(cfg api.LogConfig) logstream.Client {
	if cfg.URL != "" {
		return remote.NewHTTPClient(cfg.URL, cfg.AccountID, cfg.Token, cfg.IndirectUpload, false)
	} else {
		fmt.Println("creating a filestore client...")
		return filestore.New(pipeline.SharedVolPath)
	}
}

func (e *StepExecutorStateless) executeStep(r *api.StartStepRequest, cfg *spec.PipelineConfig) (*runtime.State, map[string]string, //nolint:gocritic
	map[string]string, []byte, []*api.OutputV2, string, error) {

	runFunc := func(ctx context.Context, step *spec.Step, output io.Writer, isDrone bool) (*runtime.State, error) {
		return engine.RunStep(ctx, engine.Opts{}, step, output, cfg, isDrone)
	}

	// Create a log stream for step logs
	// TODO (VISTAAR): Create a new log streaming client here.
	client := getLogServiceClient(r.LogConfig)
	wc := livelog.New(client, r.LogKey, r.Name, getNudges(), false)
	wr := logstream.NewReplacer(wc, r.Secrets)
	go wr.Open() //nolint:errcheck

	tiConfig := getTiCfg(&r.TIConfig)

	// if the step is configured as a daemon, it is detached
	// from the main process and executed separately.
	// We do here only for non-container step.
	if r.Detach && r.Image == "" {
		go func() {
			ctx := context.Background()
			var cancel context.CancelFunc
			if r.Timeout > 0 {
				ctx, cancel = context.WithTimeout(ctx, time.Second*time.Duration(r.Timeout))
				defer cancel()
			}
			e.run(ctx, runFunc, r, wr, &tiConfig) //nolint:errcheck
			wr.Close()
		}()
		return &runtime.State{Exited: false}, nil, nil, nil, nil, "", nil
	}

	var result error

	ctx := context.Background()
	var cancel context.CancelFunc
	if r.Timeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, time.Second*time.Duration(r.Timeout))
		defer cancel()
	}

	exited, outputs, envs, artifact, outputV2, optimizationState, err := e.run(ctx, runFunc, r, wr, &tiConfig)
	if err != nil {
		result = multierror.Append(result, err)
	}

	// if err is not nill or it's not a detach step then always close the stream
	if err != nil || !r.Detach {
		// close the stream. If the session is a remote session, the
		// full log buffer is uploaded to the remote server.
		if err = wr.Close(); err != nil {
			result = multierror.Append(result, err)
		}
	}

	// if the context was canceled and returns a canceled or
	// DeadlineExceeded error this indicates the step was timed out.
	switch ctx.Err() {
	case context.Canceled, context.DeadlineExceeded:
		return nil, nil, nil, nil, nil, "", ctx.Err()
	}

	if exited != nil {
		if exited.ExitCode != 0 {
			if wc.Error() != nil {
				result = multierror.Append(result, err)
			}
		}

		if exited.OOMKilled {
			logrus.WithField("id", r.ID).Infoln("received oom kill.")
		} else {
			logrus.WithField("id", r.ID).Infof("received exit code %d\n", exited.ExitCode)
		}
	}
	return exited, outputs, envs, artifact, outputV2, optimizationState, result
}

func (e *StepExecutorStateless) run(ctx context.Context, f RunFunc, r *api.StartStepRequest, out io.Writer, tiConfig *tiCfg.Cfg) ( //nolint:gocritic
	*runtime.State, map[string]string, map[string]string, []byte, []*api.OutputV2, string, error) {
	if r.Kind == api.Run {
		return executeRunStep(ctx, f, r, out, tiConfig)
	}
	if r.Kind == api.RunTestsV2 {
		return executeRunTestsV2Step(ctx, f, r, out, tiConfig)
	}
	return executeRunTestStep(ctx, f, r, out, tiConfig)
}

func getTiCfg(t *api.TIConfig) tiCfg.Cfg {
	cfg := tiCfg.New(t.URL, t.Token, t.AccountID, t.OrgID, t.ProjectID, t.PipelineID, t.BuildID, t.StageID, t.Repo,
		t.Sha, t.CommitLink, t.SourceBranch, t.TargetBranch, t.CommitBranch, pipeline.SharedVolPath, t.ParseSavings, false)
	return cfg
}
