package executor

import (
	"context"
	"sync"
	"time"

	"github.com/harness/lite-engine/api"
	"github.com/harness/lite-engine/engine"
	"github.com/harness/lite-engine/errors"
	"github.com/harness/lite-engine/livelog"
	"github.com/harness/lite-engine/logstream"
	"github.com/harness/lite-engine/pipeline"
	"github.com/hashicorp/go-multierror"
	"github.com/sirupsen/logrus"

	"github.com/drone/runner-go/pipeline/runtime"
)

type ExecutionStatus int

type StepStatus struct {
	Status  ExecutionStatus
	State   *runtime.State
	StepErr error
}

const (
	NotStarted ExecutionStatus = iota
	Running
	Complete
)

type StepExecutor struct {
	engine     *engine.Engine
	mu         sync.Mutex
	stepStatus map[string]StepStatus
	stepWaitCh map[string][]chan StepStatus
}

func NewStepExecutor(engine *engine.Engine) *StepExecutor {
	return &StepExecutor{
		engine:     engine,
		mu:         sync.Mutex{},
		stepWaitCh: make(map[string][]chan StepStatus),
		stepStatus: make(map[string]StepStatus),
	}
}

func (e *StepExecutor) StartStep(ctx context.Context, r *api.StartStepRequest) error {
	if r.ID == "" {
		return &errors.BadRequestError{Msg: "ID needs to be set"}
	}

	e.mu.Lock()
	_, ok := e.stepStatus[r.ID]
	if ok {
		e.mu.Unlock()
		return nil
	}

	e.stepStatus[r.ID] = StepStatus{Status: Running}
	e.mu.Unlock()

	go func() {
		state, stepErr := e.executeStep(r)
		status := StepStatus{Status: Complete, State: state, StepErr: stepErr}
		e.mu.Lock()
		e.stepStatus[r.ID] = status
		channels := e.stepWaitCh[r.ID]
		e.mu.Unlock()

		for _, ch := range channels {
			ch <- status
		}
	}()
	return nil
}

func (e *StepExecutor) PollStep(ctx context.Context, r *api.PollStepRequest) (*api.PollStepResponse, error) {
	id := r.ID
	if r.ID == "" {
		return &api.PollStepResponse{}, &errors.BadRequestError{Msg: "ID needs to be set"}
	}

	e.mu.Lock()
	s, ok := e.stepStatus[id]
	if !ok {
		e.mu.Unlock()
		return &api.PollStepResponse{}, &errors.BadRequestError{Msg: "Step has not started"}
	}

	if s.Status == Complete {
		e.mu.Unlock()
		if s.StepErr != nil {
			return &api.PollStepResponse{}, &errors.InternalServerError{Msg: s.StepErr.Error()}
		}

		return &api.PollStepResponse{
			Exited:    s.State.Exited,
			ExitCode:  s.State.ExitCode,
			OOMKilled: s.State.OOMKilled,
		}, nil
	}

	ch := make(chan StepStatus, 1)
	if _, ok := e.stepWaitCh[id]; !ok {
		e.stepWaitCh[id] = append(e.stepWaitCh[id], ch)
	} else {
		e.stepWaitCh[id] = []chan StepStatus{ch}
	}
	e.mu.Unlock()

	status := <-ch
	if status.StepErr != nil {
		return &api.PollStepResponse{}, &errors.InternalServerError{Msg: status.StepErr.Error()}
	}
	return &api.PollStepResponse{
		Exited:    status.State.Exited,
		ExitCode:  status.State.ExitCode,
		OOMKilled: status.State.OOMKilled,
	}, nil
}

func (e *StepExecutor) executeStep(r *api.StartStepRequest) (*runtime.State, error) {
	state := pipeline.GetState()
	secrets := append(state.GetSecrets(), r.Secrets...)

	// Create a log stream for step logs
	client := state.GetLogStreamClient()
	wc := livelog.New(client, r.LogKey, getNudges())
	wr := logstream.NewReplacer(wc, secrets)
	go wr.Open() // nolint:errcheck

	// if the step is configured as a daemon, it is detached
	// from the main process and executed separately.
	if r.Detach {
		go func() {
			ctx := context.Background()
			var cancel context.CancelFunc
			if r.Timeout > 0 {
				ctx, cancel = context.WithTimeout(ctx, time.Second*time.Duration(r.Timeout))
				defer cancel()
			}
			executeRunStep(ctx, e.engine, r, wr) // nolint:errcheck
			wc.Close()
		}()
		return &runtime.State{Exited: false}, nil
	}

	var result error

	ctx := context.Background()
	var cancel context.CancelFunc
	if r.Timeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, time.Second*time.Duration(r.Timeout))
		defer cancel()
	}

	exited, stepErr := executeRunStep(ctx, e.engine, r, wr)
	if stepErr != nil {
		result = multierror.Append(result, stepErr)
	}

	// close the stream. If the session is a remote session, the
	// full log buffer is uploaded to the remote server.
	if err := wc.Close(); err != nil {
		result = multierror.Append(result, err)
	}

	// if the context was canceled and returns a canceled or
	// DeadlineExceeded error this indicates the pipeline was
	// canceled.
	switch ctx.Err() {
	case context.Canceled, context.DeadlineExceeded:
		return nil, ctx.Err()
	}

	if exited != nil {
		if exited.OOMKilled {
			logrus.WithField("id", r.ID).Infoln("received oom kill.")
		} else {
			logrus.WithField("id", r.ID).Infof("received exit code %d\n", exited.ExitCode)
		}
	}
	return exited, result
}
