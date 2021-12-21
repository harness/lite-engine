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
	for _, f := range pipelineConfig.Files {
		if f.Path != "" {
			path := f.Path
			if _, err := os.Stat(path); err == nil {
				continue
			}
			if f.IsDir {
				// create a folder
				if err := os.MkdirAll(path, fs.FileMode(f.Mode)); err != nil {
					return errors.Wrap(err,
						fmt.Sprintf("failed to create directory for host path: %s", path))
				}
			} else {
				// create a file
				newFile, newErr := os.Create(path)
				if newErr != nil {
					return errors.Wrap(newErr,
						fmt.Sprintf("failed to create file for host path: %s", path))
				}
				permErr := os.Chmod(path, fs.FileMode(f.Mode))
				if permErr != nil {
					return errors.Wrap(newErr,
						fmt.Sprintf("failed to change permissions for file on host path: %s", path))
				}
				_, writeErr := newFile.WriteString(f.Data)
				if writeErr != nil {
					newFile.Close()
					return errors.Wrap(writeErr,
						fmt.Sprintf("failed to write file for host path: %s", path))
				}
				newFile.Close()
			}
		}
	}

	for _, vol := range pipelineConfig.Volumes {
		if vol != nil && vol.HostPath != nil {
			path := vol.HostPath.Path
			vol.HostPath.Path = pathConverter(path)

			if _, err := os.Stat(path); err == nil {
				continue
			}

			if err := os.MkdirAll(path, 0777); err != nil { // nolint:gomnd
				return errors.Wrap(err,
					fmt.Sprintf("failed to create directory for host volume path: %s", path))
			}
		}
	}

	e.mu.Lock()
	e.pipelineConfig = pipelineConfig
	e.mu.Unlock()

	return e.docker.Setup(ctx, pipelineConfig)
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
	for k, v := range cfg.Envs {
		envs[k] = v
	}
	for k, v := range step.Envs {
		envs[k] = v
	}
	step.Envs = envs
	step.WorkingDir = pathConverter(step.WorkingDir)

	for _, vol := range step.Volumes {
		vol.Path = pathConverter(vol.Path)
	}

	if step.Image != "" {
		return e.docker.Run(ctx, cfg, step, output)
	}

	return exec.Run(ctx, step, output)
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
