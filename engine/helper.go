// Copyright 2022 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package engine

import (
	"context"
	"errors"
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
	if !dockerSetupEnabled(pipelineConfig) {
		return setupHelper(pipelineConfig)
	}
	d, err := docker.NewEnv(opts.Opts)
	if err != nil {
		return err
	}
	if err := setupHelper(pipelineConfig); err != nil {
		return err
	}

	return d.Setup(ctx, pipelineConfig)
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
	if !dockerSetupEnabled(cfg) {
		if cfg == nil || !cfg.PrivateConnectivity {
			_ = destroyHelper(cfg)
			return nil
		}
		return destroyHelper(cfg)
	}
	d, err := docker.NewEnv(opts.Opts)
	if err != nil {
		return err
	}
	if cfg == nil || !cfg.PrivateConnectivity {
		_ = destroyHelper(cfg)
		return d.DestroyContainersByLabel(ctx, cfg, labelKey, labelValue)
	}
	destroyErr := d.DestroyContainersByLabel(ctx, cfg, labelKey, labelValue)
	volumeErr := destroyHelper(cfg)
	return errors.Join(destroyErr, volumeErr)
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
		applyPrivateConnectivityDNS(cfg, step)
		return d.Run(ctx, cfg, step, output, isDrone, isHosted)
	}

	return exec.Run(ctx, step, output)
}

func dockerSetupEnabled(cfg *spec.PipelineConfig) bool {
	return cfg != nil && (cfg.EnableDockerSetup == nil || *cfg.EnableDockerSetup)
}

func applyPrivateConnectivityDNS(cfg *spec.PipelineConfig, step *spec.Step) {
	const quad100 = "100.100.100.100"

	// Quad100 is served by tailscaled on the host and is reachable from containers on
	// Linux (bridge) and Windows (nat). macOS has no Docker steps, so it is excluded.
	// An explicit step DNS list is a customer override; preserve it exactly instead of
	// silently changing resolver precedence. Such a step is responsible for resolving
	// the private names it uses through its selected DNS servers.
	if cfg == nil || step == nil || !cfg.PrivateConnectivity ||
		(cfg.Platform.OS != "linux" && cfg.Platform.OS != "windows") || len(step.DNS) > 0 {
		return
	}
	step.DNS = []string{quad100}
}
