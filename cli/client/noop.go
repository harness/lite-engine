package client

import (
	"context"
	"io"
	"time"

	"github.com/harness/lite-engine/api"
)

var _ Client = (*NoopClient)(nil)

type NoopClient struct {
	stepResponse *api.PollStepResponse // response to return for the step execution
	stepErr      error                 // if there's an error in polling the step response
	stepExecTime time.Duration         // how long to wait for the step to return back
	setupTime    time.Duration         // time taken to setup the stage
	destroyTime  time.Duration         // time taken to destroy the stage
}

func NewNoopClient(r *api.PollStepResponse, err error, stepExecTime, setupTime, destroyTime time.Duration) *NoopClient {
	return &NoopClient{
		stepResponse: r,
		stepErr:      err,
		stepExecTime: stepExecTime,
		setupTime:    setupTime,
		destroyTime:  destroyTime,
	}
}

func (n *NoopClient) Setup(ctx context.Context, in *api.SetupRequest) (*api.SetupResponse, error) {
	time.Sleep(n.setupTime)
	return &api.SetupResponse{}, nil
}

func (n *NoopClient) RetrySetup(ctx context.Context, in *api.SetupRequest, timeout time.Duration) (*api.SetupResponse, error) {
	return &api.SetupResponse{}, nil
}

func (n *NoopClient) Destroy(ctx context.Context, in *api.DestroyRequest) (*api.DestroyResponse, error) {
	time.Sleep(n.destroyTime)
	return &api.DestroyResponse{}, nil
}

func (*NoopClient) StartStep(ctx context.Context, in *api.StartStepRequest) (*api.StartStepResponse, error) {
	return &api.StartStepResponse{}, nil
}

func (*NoopClient) RetryStartStep(ctx context.Context, in *api.StartStepRequest) (*api.StartStepResponse, error) {
	return &api.StartStepResponse{}, nil
}

func (n *NoopClient) PollStep(ctx context.Context, in *api.PollStepRequest) (*api.PollStepResponse, error) {
	time.Sleep(n.stepExecTime)
	return n.stepResponse, n.stepErr
}

func (n *NoopClient) RetryPollStep(ctx context.Context, in *api.PollStepRequest, timeout time.Duration) (step *api.PollStepResponse, pollError error) {
	return n.PollStep(ctx, in)
}

func (*NoopClient) GetStepLogOutput(ctx context.Context, in *api.StreamOutputRequest, w io.Writer) error {
	return nil
}

func (*NoopClient) Health(ctx context.Context, in *api.HealthRequest) (*api.HealthResponse, error) {
	return &api.HealthResponse{OK: true, Version: "noop"}, nil
}

func (n *NoopClient) RetryHealth(ctx context.Context, in *api.HealthRequest) (*api.HealthResponse, error) {
	return n.Health(ctx, in)
}

func (n *NoopClient) RetrySuspend(ctx context.Context, request *api.SuspendRequest, timeout time.Duration) (*api.SuspendResponse, error) {
	return &api.SuspendResponse{}, nil
}
