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
	"github.com/harness/lite-engine/logstream"
	"github.com/harness/lite-engine/pipeline"
	"github.com/sirupsen/logrus"
)

const (
	DockerSockVolName      = "_docker"
	DockerSockUnixPath     = "/var/run/docker.sock"
	DockerSockWinPath      = `\\.\pipe\docker_engine`
	permissions            = 0777
	defaultFilePermissions = 0644 // File permissions (rw-r--r--)
	defaultDirPermissions  = 0755 // Directory permissions (rwxr-xr-x)
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
		return fmt.Errorf("failed to create files/folders for pipeline %v: %w", pipelineConfig.Files, err)
	}
	// create volumes
	for _, vol := range pipelineConfig.Volumes {
		if vol == nil || vol.HostPath == nil {
			continue
		}
		path := vol.HostPath.Path
		vol.HostPath.Path = PathConverter(path)

		if _, err := os.Stat(path); err == nil {
			_ = os.Chmod(path, permissions)
			continue
		}

		if err := os.MkdirAll(path, permissions); err != nil {
			return fmt.Errorf("failed to create directory for host volume path: %q: %w", path, err)
		}
		_ = os.Chmod(path, permissions)
	}

	// create mTLS certs and set environment variable if successful
	certsWritten, err := createMtlsCerts(pipelineConfig.MtlsConfig)
	if err != nil {
		return fmt.Errorf("failed to create mTLS certificates: %w", err)
	}
	if certsWritten {
		// This can be used by STO and SSCA plugins to support mTLS
		pipelineConfig.Envs["HARNESS_MTLS_CERTS_DIR"] = pipelineConfig.MtlsConfig.ClientCertDirPath
	}

	// Load sanitize patterns directly into memory (in-memory only, no disk persistence)
	// Patterns come from delegate and are loaded into runtime for immediate use
	if err := loadSanitizePatternsIntoRuntime(pipelineConfig.SanitizeConfig); err != nil {
		// Log warning but don't fail setup - sanitization is optional
		logrus.WithError(err).Warn("failed to load sanitize patterns into runtime")
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
		return false, fmt.Errorf("failed to create mTLS directory: %w", err)
	}

	// Decode and write certificate
	certPath := filepath.Join(mtlsConfig.ClientCertDirPath, "client.crt")
	if err := writeBase64ToFile(certPath, mtlsConfig.ClientCert); err != nil {
		return false, fmt.Errorf("failed to write mTLS certificate: %w", err)
	}

	// Set 0777 permissions for the certificate
	if _, err := os.Stat(certPath); err == nil {
		if err := os.Chmod(certPath, permissions); err != nil {
			logrus.Errorf("Failed to set permissions %o for file on host path: %q: %v", permissions, certPath, err)
		}
	}

	// Decode and write key
	keyPath := filepath.Join(mtlsConfig.ClientCertDirPath, "client.key")
	if err := writeBase64ToFile(keyPath, mtlsConfig.ClientCertKey); err != nil {
		return false, fmt.Errorf("failed to write mTLS key: %w", err)
	}

	// Set 0777 permissions for the key
	if _, err := os.Stat(keyPath); err == nil {
		if err := os.Chmod(keyPath, permissions); err != nil {
			logrus.Errorf("Failed to set permissions %o for file on host path: %q: %v", permissions, keyPath, err)
		}
	}

	return true, nil
}

// loadSanitizePatternsIntoRuntime loads sanitize patterns directly into the runtime sanitizer
// Decodes Base64 content and loads patterns in-memory (no disk write required)
func loadSanitizePatternsIntoRuntime(sanitizeConfig spec.SanitizeConfig) error {
	if sanitizeConfig.SanitizePatternsContent == "" {
		logrus.Info("no sanitize patterns provided from delegate (SanitizePatternsContent is empty)")
		return nil // No patterns to load
	}

	// Decode Base64 content
	data, err := base64.StdEncoding.DecodeString(sanitizeConfig.SanitizePatternsContent)
	if err != nil {
		return fmt.Errorf("failed to decode sanitize patterns from Base64: %w", err)
	}

	// Load patterns directly from decoded string content (in-memory)
	content := string(data)

	// Count patterns (newline-separated)
	patternLines := strings.Split(strings.TrimSpace(content), "\n")
	patternCount := 0
	for _, line := range patternLines {
		if strings.TrimSpace(line) != "" {
			patternCount++
		}
	}

	if err := logstream.LoadCustomPatternsFromString(content); err != nil {
		return fmt.Errorf("failed to load sanitize patterns into runtime: %w", err)
	}

	logrus.WithField("pattern_count", patternCount).Info("successfully loaded sanitize patterns from delegate into runtime (in-memory)")
	return nil
}

// writeBase64ToFile decodes base64 data and writes it to a file
func writeBase64ToFile(filePath, base64Data string) error {
	data, err := base64.StdEncoding.DecodeString(base64Data)
	if err != nil {
		return fmt.Errorf("failed to decode base64 data: %w", err)
	}

	if err := os.WriteFile(filePath, data, permissions); err != nil {
		return fmt.Errorf("failed to write to file: %s: %w", filePath, err)
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

	return exec.Run(ctx, step, output)
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
	step.WorkingDir = PathConverter(step.WorkingDir)

	// create files or folders specific to the step
	if err := createFiles(step.Files); err != nil {
		return err
	}

	// If we are running on Windows, convert
	// the volume paths to windows paths
	for _, vol := range step.Volumes {
		vol.Path = PathConverter(vol.Path)
	}

	for _, vol := range cfg.Volumes {
		if vol == nil || vol.HostPath == nil {
			continue
		}
		vol.HostPath.Path = PathConverter(vol.HostPath.Path)
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
				logrus.Errorf("failed to set permissions for file on host path: %q: %v", path, err)
				continue
			}
		}

		if f.IsDir {
			// create a folder
			if err := os.MkdirAll(path, fs.FileMode(f.Mode)); err != nil {
				return fmt.Errorf("failed to create directory for host path: %q: %w", path, err)
			}
			continue
		}

		// For creating directories if not exists
		dir := filepath.Dir(path)
		if _, err := os.Stat(dir); os.IsNotExist(err) {
			if err := os.MkdirAll(dir, fs.FileMode(permissions)); err != nil {
				return fmt.Errorf("failed to create directory for path %q: %w", path, err)
			}
		}

		// Create (or overwrite) the file
		file, err := os.Create(path)
		if err != nil {
			return fmt.Errorf("failed to create file for host path: %q: %w", path, err)
		}

		if _, err = file.WriteString(f.Data); err != nil {
			_ = file.Close()
			return fmt.Errorf("failed to write file for host path: %q: %w", path, err)
		}

		_ = file.Close()

		if err = os.Chmod(path, fs.FileMode(f.Mode)); err != nil {
			return fmt.Errorf("failed to change permissions for file on host path: %q: %w", path, err)
		}
	}
	return nil
}

// PathConverter converts Unix-style paths to Windows format on Windows OS.
// This function is used throughout the codebase for consistent path handling.
func PathConverter(path string) string {
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
