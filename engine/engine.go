package engine

import (
	"context"
	"io"

	"github.com/drone/runner-go/pipeline/runtime"
	"github.com/harness/lite-engine/engine/docker"
	"github.com/harness/lite-engine/engine/exec"
	"github.com/harness/lite-engine/engine/spec"
)

type Engine struct {
	pipelineConfig *spec.PipelineConfig
	docker         *docker.Docker
}

func NewEnv(opts docker.Opts) (*Engine, error) {
	d, err := docker.NewEnv(opts)
	if err != nil {
		return nil, err
	}
	return &Engine{
		pipelineConfig: &spec.PipelineConfig{},
		docker:         d,
	}, nil
}

func (e *Engine) Setup(ctx context.Context, pipelineConfig *spec.PipelineConfig) error {
	e.pipelineConfig = pipelineConfig

	return e.docker.Setup(ctx, e.pipelineConfig)
}

func (e *Engine) Destroy(ctx context.Context) error {
	return e.docker.Destroy(ctx, e.pipelineConfig)
}

func (e *Engine) Run(ctx context.Context, step *spec.Step, output io.Writer) (*runtime.State, error) {
	for k, v := range e.pipelineConfig.Envs {
		step.Envs[k] = v
	}

	if step.Image != "" {
		return e.docker.Run(ctx, e.pipelineConfig, step, output)
	}

	return exec.Run(ctx, step, output)
}
