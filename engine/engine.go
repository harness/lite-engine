// Copyright 2022 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package engine

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	osruntime "runtime"
	"strings"
	"sync"

	"github.com/drone/runner-go/pipeline/runtime"
	"github.com/harness/lite-engine/common/external"
	"github.com/harness/lite-engine/engine/docker"
	"github.com/harness/lite-engine/engine/exec"
	"github.com/harness/lite-engine/engine/spec"
	"github.com/harness/lite-engine/pipeline"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

const (
	DockerSockVolName      = "_docker"
	DockerSockUnixPath     = "/var/run/docker.sock"
	DockerSockWinPath      = `\\.\pipe\docker_engine`
	permissions            = 0777
	defaultFilePermissions = 0644 // File permissions (rw-r--r--)
	boldYellowColor        = "\u001b[33;1m"
	trueValue              = "true"
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

func setupHelper(pipelineConfig *spec.PipelineConfig) error {
	// create global files and folders
	if err := createFiles(pipelineConfig.Files); err != nil {
		return errors.Wrap(err,
			fmt.Sprintf("failed to create files/folders for pipeline %v", pipelineConfig.Files))
	}
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

	// create mTLS certs and set environment variable if successful
	certsWritten, err := createMtlsCerts(pipelineConfig.MtlsConfig)
	if err != nil {
		return errors.Wrap(err, "failed to create mTLS certificates")
	}
	if certsWritten {
		// This can be used by STO and SSCA plugins to support mTLS
		pipelineConfig.Envs["HARNESS_MTLS_CERTS_DIR"] = pipelineConfig.MtlsConfig.ClientCertDirPath
	}

	return nil
}

// createMtlsCerts handles creation of mTLS certificates from base64-encoded data
func createMtlsCerts(mtlsConfig spec.MtlsConfig) (bool, error) {
	if mtlsConfig.ClientCert == "" || mtlsConfig.ClientCertKey == "" || mtlsConfig.ClientCertDirPath == "" {
		return false, nil // No certs to process or dir path not set
	}

	// Create the mTLS directory
	if err := os.MkdirAll(mtlsConfig.ClientCertDirPath, permissions); err != nil {
		return false, errors.Wrap(err, "failed to create mTLS directory")
	}

	// Decode and write certificate
	certPath := filepath.Join(mtlsConfig.ClientCertDirPath, "client.crt")
	if err := writeBase64ToFile(certPath, mtlsConfig.ClientCert); err != nil {
		return false, errors.Wrap(err, "failed to write mTLS certificate")
	}

	// Set 0777 permissions for the certificate
	if _, err := os.Stat(certPath); err == nil {
		if err := os.Chmod(certPath, permissions); err != nil {
			logrus.Error(errors.Wrap(err,
				fmt.Sprintf("Failed to set permissions %o for file on host path: %q", permissions, certPath)))
		}
	}

	// Decode and write key
	keyPath := filepath.Join(mtlsConfig.ClientCertDirPath, "client.key")
	if err := writeBase64ToFile(keyPath, mtlsConfig.ClientCertKey); err != nil {
		return false, errors.Wrap(err, "failed to write mTLS key")
	}

	// Set 0777 permissions for the key
	if _, err := os.Stat(keyPath); err == nil {
		if err := os.Chmod(keyPath, permissions); err != nil {
			logrus.Error(errors.Wrap(err,
				fmt.Sprintf("Failed to set permissions %o for file on host path: %q", permissions, certPath)))
		}
	}

	return true, nil
}

// writeBase64ToFile decodes base64 data and writes it to a file
func writeBase64ToFile(filePath, base64Data string) error {
	data, err := base64.StdEncoding.DecodeString(base64Data)
	if err != nil {
		return errors.Wrap(err, "failed to decode base64 data")
	}

	if err := os.WriteFile(filePath, data, permissions); err != nil {
		return errors.Wrap(err, fmt.Sprintf("failed to write to file: %s", filePath))
	}

	return nil
}

func (e *Engine) Setup(ctx context.Context, pipelineConfig *spec.PipelineConfig) error {
	if err := setupHelper(pipelineConfig); err != nil {
		return err
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
	destroyHelper(cfg)

	return e.docker.Destroy(ctx, cfg)
}

func (e *Engine) Run(ctx context.Context, step *spec.Step, output io.Writer, isDrone, isHosted bool) (*runtime.State, error) {
	e.mu.Lock()
	cfg := e.pipelineConfig
	e.mu.Unlock()

	if err := runHelper(cfg, step); err != nil {
		return nil, err
	}

	if !isDrone && len(step.Command) > 0 {
		printCommand(step, output)
	}

	if step.Image != "" {
		return e.docker.Run(ctx, cfg, step, output, isDrone, isHosted)
	}

	return exec.Run(ctx, step, output, "")
}

func (e *Engine) Suspend(ctx context.Context, labels map[string]string) error {
	return e.docker.Suspend(ctx, labels)
}

func destroyHelper(cfg *spec.PipelineConfig) {
	for _, vol := range cfg.Volumes {
		if vol == nil || vol.HostPath == nil {
			continue
		}
		if !vol.HostPath.Remove {
			continue
		}

		// TODO: Add logging
		path := vol.HostPath.Path
		os.RemoveAll(path)
	}
}

func runHelper(cfg *spec.PipelineConfig, step *spec.Step) error {
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
		return err
	}

	for _, vol := range step.Volumes {
		vol.Path = pathConverter(vol.Path)
	}
	return nil
}

// collectAllSecrets collects secrets from all available sources
func collectAllSecrets(step *spec.Step) []string {
	var allSecrets []string

	// Get secrets from pipeline state
	pipelineState := pipeline.GetState()
	if pipelineState != nil {
		allSecrets = append(allSecrets, pipelineState.GetSecrets()...)
	}

	// Get secrets from step-level secrets
	for _, secret := range step.Secrets {
		if len(secret.Data) > 0 {
			allSecrets = append(allSecrets, string(secret.Data))
		}
	}

	return allSecrets
}

// maskCommandWithReplacer masks secrets in the command string with environment variable support
func maskCommandWithReplacer(command string, step *spec.Step) string {
	allSecrets := collectAllSecrets(step)
	if len(allSecrets) == 0 {
		return command
	}
	return external.MaskStringWithEnvs(command, allSecrets, step.Envs)
}

func printCommand(step *spec.Step, output io.Writer) {
	stepCommand := strings.TrimSpace(strings.Join(step.Command, ""))
	if stepCommand != "" {
		printCommand := ""
		if val, ok := step.Envs["CI_ENABLE_EXTRA_CHARACTERS_SECRETS_MASKING"]; ok && val == trueValue {
			maskedCommand := maskCommandWithReplacer(stepCommand, step)
			printCommand = "Executing the following masked command(s):\n" + maskedCommand
		} else {
			printCommand = "Executing the following command(s):\n" + stepCommand
		}
		lines := strings.Split(printCommand, "\n")
		for _, line := range lines {
			_, _ = output.Write([]byte(boldYellowColor + line + "\n"))
		}
	}
}

func createFiles(paths []*spec.File) error {
	for _, f := range paths {
		if f.Path == "" {
			continue
		}

		path := f.Path

		// make the file writable (if it exists)
		if _, err := os.Stat(path); err == nil {
			if err = os.Chmod(path, defaultFilePermissions); err != nil {
				logrus.Error(errors.Wrap(err,
					fmt.Sprintf("failed to set permissions for file on host path: %q", path)))
				continue
			}
		}

		if f.IsDir {
			// create a folder
			if err := os.MkdirAll(path, fs.FileMode(f.Mode)); err != nil {
				return errors.Wrap(err,
					fmt.Sprintf("failed to create directory for host path: %q", path))
			}
			continue
		}

		// For creating directories if not exists
		dir := filepath.Dir(path)
		if _, err := os.Stat(dir); os.IsNotExist(err) {
			if err := os.MkdirAll(dir, fs.FileMode(permissions)); err != nil {
				return errors.Wrap(err, fmt.Sprintf("failed to create directory: for path %q", path))
			}
		}

		// Create (or overwrite) the file
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
