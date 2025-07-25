// Copyright 2022 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package engine

import (
	"context"
	"io"

	"github.com/drone/runner-go/pipeline/runtime"
	"github.com/harness/lite-engine/engine/docker"
	"github.com/harness/lite-engine/engine/exec"
	"github.com/harness/lite-engine/engine/spec"
)

type Opts struct {
	docker.Opts
}

// SetupPipeline is a helper function to setup a pipeline given a pipeline configuration.
func SetupPipeline(
	ctx context.Context,
	opts Opts,
	pipelineConfig *spec.PipelineConfig,
) error {
	d, err := docker.NewEnv(opts.Opts)
	if err != nil {
		return err
	}
	if err := setupHelper(pipelineConfig); err != nil {
		return err
	}

	// required to support m1 where docker isn't installed.
	if pipelineConfig.EnableDockerSetup == nil || *pipelineConfig.EnableDockerSetup {
		return d.Setup(ctx, pipelineConfig)
	}
	return nil
}

// DestroyPipeline is a helper function to destroy a pipeline given a pipeline configuration.
// The labelKey and labelValue are used to identify the containers to destroy.
func DestroyPipeline(
	ctx context.Context,
	opts Opts,
	cfg *spec.PipelineConfig,
	labelKey string, // label to use if containers need to be destroyed
	labelValue string,
) error {
	d, err := docker.NewEnv(opts.Opts)
	if err != nil {
		return err
	}
	destroyHelper(cfg)
	return d.DestroyContainersByLabel(ctx, cfg, labelKey, labelValue)
}

// RunStep executes a step in a pipeline. It takes a pipeline configuration and a step configuration
// as input. The pipeline configuration is used today for things like looking up volumes and using
// pipeline-level environment variables.
func RunStep(
	ctx context.Context,
	opts Opts,
	step *spec.Step,
	output io.Writer,
	cfg *spec.PipelineConfig,
	isDrone bool,
	isHosted bool,
) (*runtime.State, error) {
	d, err := docker.NewEnv(opts.Opts)
	if err != nil {
		return nil, err
	}

	if err := runHelper(cfg, step); err != nil {
		return nil, err
	}

	if !isDrone && len(step.Command) > 0 {
		printCommand(step, output)
	}
	if step.Image != "" {
		return d.Run(ctx, cfg, step, output, isDrone, isHosted)
	}

	return exec.Run(ctx, step, output, cfg.ProcessIdsFilePath)
}
