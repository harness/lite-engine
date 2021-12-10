package engine

import (
	"context"
	"fmt"
	"io"
	"os"
	"sync"

	"github.com/drone/runner-go/pipeline/runtime"
	"github.com/harness/lite-engine/engine/docker"
	"github.com/harness/lite-engine/engine/exec"
	"github.com/harness/lite-engine/engine/spec"
	"github.com/pkg/errors"
)

type Engine struct {
	pipelineConfig *spec.PipelineConfig
	docker         *docker.Docker
	mu             sync.Mutex
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
	e.mu.Lock()
	e.pipelineConfig = pipelineConfig
	e.mu.Unlock()

	for _, vol := range pipelineConfig.Volumes {
		if vol != nil && vol.HostPath != nil {
			if err := os.MkdirAll(vol.HostPath.Path, 0777); err != nil { // nolint:gomnd
				return errors.Wrap(err,
					fmt.Sprintf("failed to create directory for host volume path: %s", vol.HostPath.Path))
			}
		}
	}

	return e.docker.Setup(ctx, e.pipelineConfig)
}

func (e *Engine) Destroy(ctx context.Context) error {
	e.mu.Lock()
	cfg := e.pipelineConfig
	e.mu.Unlock()

	return e.docker.Destroy(ctx, cfg)
}

func (e *Engine) Run(ctx context.Context, step *spec.Step, output io.Writer) (*runtime.State, error) {
	e.mu.Lock()
	cfg := e.pipelineConfig
	e.mu.Unlock()

	if step.Envs == nil {
		step.Envs = make(map[string]string)
	}
	for k, v := range cfg.Envs {
		step.Envs[k] = v
	}

	if step.Image != "" {
		return e.docker.Run(ctx, cfg, step, output)
	}

	return exec.Run(ctx, step, output)
}
