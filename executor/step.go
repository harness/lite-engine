package executor

import (
	"context"
	"fmt"
	"sync"

	"github.com/harness/lite-engine/api"
	"github.com/harness/lite-engine/engine"
	"github.com/harness/lite-engine/errors"

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
	if r.ExecutionID == "" {
		return &errors.BadRequestError{Msg: "Execution ID needs to be set"}
	}

	e.mu.Lock()
	_, ok := e.stepStatus[r.ExecutionID]
	if ok {
		return nil
	}

	e.stepStatus[r.ExecutionID] = StepStatus{Status: Running}
	e.mu.Unlock()

	go func() {
		var state *runtime.State
		var stepErr error
		if r.Kind == api.Run {
			state, stepErr = executeRunStep(context.Background(), e.engine, r)
		} else {
			executeRunTestStep()
		}

		status := StepStatus{Status: Complete, State: state, StepErr: stepErr}
		e.mu.Lock()
		e.stepStatus[r.ExecutionID] = status
		channels := e.stepWaitCh[r.ExecutionID]
		e.mu.Unlock()

		for _, ch := range channels {
			ch <- status
		}
	}()
	return nil
}

func (e *StepExecutor) PollStep(ctx context.Context, r *api.PollStepRequest) (*api.PollStepResponse, error) {
	id := r.ExecutionID
	if id == "" {
		return &api.PollStepResponse{}, &errors.BadRequestError{Msg: "Execution ID needs to be set"}
	}

	e.mu.Lock()
	s, ok := e.stepStatus[id]
	if !ok {
		e.mu.Unlock()
		return &api.PollStepResponse{}, &errors.BadRequestError{Msg: "Step has not started"}
	}

	if s.Status == Complete {
		e.mu.Unlock()
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
		errMsg := fmt.Sprintf("failed to execute step with err: %s", status.StepErr.Error())
		return &api.PollStepResponse{}, &errors.InternalServerError{Msg: errMsg}
	}
	return &api.PollStepResponse{
		Exited:    status.State.Exited,
		ExitCode:  status.State.ExitCode,
		OOMKilled: status.State.OOMKilled,
	}, nil
}
