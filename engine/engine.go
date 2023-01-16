// Copyright 2022 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package engine

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	osruntime "runtime"
	"strings"
	"sync"

	"github.com/drone/runner-go/pipeline/runtime"
	"github.com/harness/lite-engine/engine/docker"
	"github.com/harness/lite-engine/engine/exec"
	"github.com/harness/lite-engine/engine/spec"
	"github.com/pkg/errors"
)

const (
	DockerSockVolName  = "_docker"
	DockerSockUnixPath = "/var/run/docker.sock"
	DockerSockWinPath  = `\\.\pipe\docker_engine`
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
	// create global files and folders
	if err := createFiles(pipelineConfig.Files); err != nil {
		return errors.Wrap(err,
			fmt.Sprintf("failed to create files/folders for pipeline %v", pipelineConfig.Files))
	}
	// create volumes
	for _, vol := range pipelineConfig.Volumes {
		if vol != nil && vol.HostPath != nil {
			path := vol.HostPath.Path
			vol.HostPath.Path = pathConverter(path)

			if _, err := os.Stat(path); err == nil {
				continue
			}

			if err := os.MkdirAll(path, 0777); err != nil { //nolint:gomnd
				return errors.Wrap(err,
					fmt.Sprintf("failed to create directory for host volume path: %q", path))
			}
		}
	}

	e.mu.Lock()
	e.pipelineConfig = pipelineConfig
	e.mu.Unlock()
	// required to support m1 where docker isn't installed.
	if e.pipelineConfig.EnableDockerSetup == nil || *e.pipelineConfig.EnableDockerSetup {
		return e.docker.Setup(ctx, pipelineConfig)
	}
	return nil
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

	if step.Image != "" {
		return e.docker.Run(ctx, cfg, step, output)
	}

	return exec.Run(ctx, step, output)
}

func createFiles(paths []*spec.File) error {
	for _, f := range paths {
		if f.Path == "" {
			continue
		}

		path := f.Path
		if _, err := os.Stat(path); err == nil {
			continue
		}

		if f.IsDir {
			// create a folder
			if err := os.MkdirAll(path, fs.FileMode(f.Mode)); err != nil {
				return errors.Wrap(err,
					fmt.Sprintf("failed to create directory for host path: %q", path))
			}

			continue
		}

		// create a file
		file, err := os.Create(path)
		if err != nil {
			return errors.Wrap(err,
				fmt.Sprintf("failed to create file for host path: %q", path))
		}

		if _, err = file.WriteString(f.Data); err != nil {
			_ = file.Close()
			return errors.Wrap(err,
				fmt.Sprintf("failed to write file for host path: %q", path))
		}

		_ = file.Close()

		if err = os.Chmod(path, fs.FileMode(f.Mode)); err != nil {
			return errors.Wrap(err,
				fmt.Sprintf("failed to change permissions for file on host path: %q", path))
		}
	}
	return nil
}

func pathConverter(path string) string {
	if osruntime.GOOS == "windows" {
		return toWindowsDrive(path)
	}
	return path
}

// helper function converts the path to a valid windows
// path, including the default C drive.
func toWindowsDrive(s string) string {
	if matchDockerSockPath(s) {
		return s
	}
	if len(s) >= 2 && (s[0] >= 'a' && s[0] <= 'z' || s[0] >= 'A' && s[0] <= 'Z') && s[1] == ':' {
		return toWindowsPath(s)
	}
	return "c:" + toWindowsPath(s)
}

// helper function converts the path to a valid windows
// path, replacing backslashes with forward slashes.
func toWindowsPath(s string) string {
	return strings.Replace(s, "/", "\\", -1)
}

func matchDockerSockPath(s string) bool {
	if s == DockerSockWinPath || s == DockerSockUnixPath {
		return true
	}
	return false
}
