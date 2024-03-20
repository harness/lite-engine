// Copyright 2022 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package engine

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/drone/runner-go/pipeline/runtime"
	"github.com/harness/lite-engine/engine/docker"
	"github.com/harness/lite-engine/engine/exec"
	"github.com/harness/lite-engine/engine/spec"
	"github.com/pkg/errors"
)

type Opts struct {
	docker.Opts
}

func SetupPipeline(
	ctx context.Context,
	opts Opts,
	pipelineConfig *spec.PipelineConfig,
) error {
	d, err := docker.NewEnv(opts.Opts)
	if err != nil {
		return err
	}
	// NOT USED
	// // create global files and folders
	// if err := createFiles(pipelineConfig.Files); err != nil {
	// 	return errors.Wrap(err,
	// 		fmt.Sprintf("failed to create files/folders for pipeline %v", pipelineConfig.Files))
	// }
	// create volumes
	for _, vol := range pipelineConfig.Volumes {
		if vol == nil || vol.HostPath == nil {
			continue
		}
		path := vol.HostPath.Path
		vol.HostPath.Path = pathConverter(path)

		if _, err := os.Stat(path); err == nil {
			_ = os.Chmod(path, permissions)
			continue
		}

		if err := os.MkdirAll(path, permissions); err != nil {
			return errors.Wrap(err,
				fmt.Sprintf("failed to create directory for host volume path: %q", path))
		}
		_ = os.Chmod(path, permissions)
	}

	// required to support m1 where docker isn't installed.
	if pipelineConfig.EnableDockerSetup == nil || *pipelineConfig.EnableDockerSetup {
		return d.Setup(ctx, pipelineConfig)
	}
	return nil
}

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
	return d.DestroyContainersByLabel(ctx, cfg, labelKey, labelValue)
}

func RunStep(
	ctx context.Context,
	opts Opts,
	step *spec.Step,
	output io.Writer,
	cfg *spec.PipelineConfig,
	isDrone bool,
) (*runtime.State, error) {
	d, err := docker.NewEnv(opts.Opts)
	if err != nil {
		return nil, err
	}
	envs := make(map[string]string)
	if step.Image == "" {
		// Set parent process envs in case step is executed directly on the VM.
		// This sets the PATH environment variable (in case it is set on parent process) on sub-process executing the step.
		for _, e := range os.Environ() {
			if i := strings.Index(e, "="); i >= 0 {
				envs[e[:i]] = e[i+1:]
			}
		}
	}
	for k, v := range cfg.Envs {
		envs[k] = v
	}
	for k, v := range step.Envs {
		envs[k] = v
	}
	step.Envs = envs
	step.WorkingDir = pathConverter(step.WorkingDir)

	// create files or folders specific to the step
	if err := createFiles(step.Files); err != nil {
		return nil, err
	}

	for _, vol := range step.Volumes {
		vol.Path = pathConverter(vol.Path)
	}

	if !isDrone && len(step.Command) > 0 {
		printCommand(step, output)
	}
	if step.Image != "" {
		return d.Run(ctx, cfg, step, output, isDrone)
	}

	return exec.Run(ctx, step, output)
}
