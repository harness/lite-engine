// Copyright 2022 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package runtime

import (
	"bufio"
	"context"
	b64 "encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	goruntime "runtime"
	"strings"
	"time"

	"github.com/drone/runner-go/pipeline/runtime"
	v2 "github.com/harness/godotenv/v2"
	v4 "github.com/harness/godotenv/v4"
	"github.com/harness/lite-engine/api"
	"github.com/harness/lite-engine/engine/spec"
	"github.com/harness/lite-engine/livelog"
	"github.com/harness/lite-engine/logstream"
	"github.com/harness/lite-engine/logstream/remote"
	"github.com/harness/lite-engine/logstream/stdout"
	tiCfg "github.com/harness/lite-engine/ti/config"
	ti "github.com/harness/ti-client/types"
	"github.com/sirupsen/logrus"
)

const (
	ciNewVersionGodotEnv = "CI_NEW_VERSION_GODOTENV"
	trueValue            = "true"

	// Windows container path where hcli is mounted (used for PATH injection)
	hcliWindowsContainerPath = `C:\harness\lite-engine`
)

func getNudges() []logstream.Nudge {
	// <search-term> <resolution> <error-msg>
	return []logstream.Nudge{
		logstream.NewNudge("[Kk]illed", "Increase memory resources for the step", errors.New("out of memory")),
		logstream.NewNudge(".*git.* SSL certificate problem",
			"Set sslVerify to false in CI codebase properties", errors.New("SSL certificate error")),
		logstream.NewNudge("Cannot connect to the Docker daemon",
			"Setup dind if it's not running. If dind is running, privileged should be set to true",
			errors.New("could not connect to the docker daemon")),
	}
}

func getOutputVarCmd(entrypoint, outputVars []string, outputFile string, useNewGoDotEnv bool) string {
	isPsh := IsPowershell(entrypoint)
	isPython := isPython(entrypoint)

	cmd := ""
	if useNewGoDotEnv {
		if isPsh {
			cmd += fmt.Sprintf("\nNew-Item %s", outputFile)
		} else if isPython {
			cmd += `
import os
import sys
import base64
def get_env_var(name):
    """Fetch an environment variable, exiting with an error if not set."""
    value = os.getenv(name)
    if value is None:
        print(f"Error: Output variable '{name}' is not set")
        sys.exit(1)
    return value
`
		}
		for _, o := range outputVars {
			if isPsh {
				cmd += fmt.Sprintf(
					"\n$envVal = if ($null -eq $Env:%s) { '' } else { $Env:%s }; "+
						"$val = '%s=__B64__' + [Convert]::ToBase64String([System.Text.Encoding]::UTF8.GetBytes($envVal)) \n"+
						"Add-Content -Path %s -Value $val",
					o, o, o, outputFile)
			} else if isPython {
				cmd += fmt.Sprintf(`
try:
    with open('%s', 'a') as out_file:
        value = get_env_var('%s')
        b64val = base64.b64encode(value.encode()).decode()
        out_file.write('%s=__B64__' + b64val + '\n')
except Exception as e:
    print(f"Error: {e}")
    sys.exit(1)
`, outputFile, o, o)
			} else {
				cmd += fmt.Sprintf("\nprintf '%%s=__B64__%%s\\n' '%s' \"$(printf '%%s' \"$%s\" | base64 | tr -d '\\n')\" >> %s",
					o,
					o,
					outputFile)
			}
		}
	} else {
		if isPsh {
			cmd += fmt.Sprintf("\nNew-Item %s", outputFile)
		} else if isPython {
			cmd += "\nimport os\n"
		}
		for _, o := range outputVars {
			if isPsh {
				cmd += fmt.Sprintf("\n$val = \"%s=$Env:%s\" \nAdd-Content -Path %s -Value $val", o, o, outputFile)
			} else if isPython {
				cmd += fmt.Sprintf("with open('%s', 'a') as out_file:\n\tout_file.write('%s=' + os.getenv('%s') + '\\n')\n", outputFile, o, o)
			} else {
				cmd += fmt.Sprintf("\necho \"%s=$%s\" >> %s", o, o, outputFile)
			}
		}
	}

	return cmd
}

func getOutputsCmd(entrypoint []string, outputVars []*api.OutputV2, outputFile string, useNewGoDotEnv bool) string {
	isPsh := IsPowershell(entrypoint)
	isPython := isPython(entrypoint)

	cmd := ""
	if useNewGoDotEnv {
		if isPsh {
			cmd += fmt.Sprintf("\nNew-Item %s", outputFile)
		} else if isPython {
			cmd += `
import os
import sys
import base64
def get_env_var(name):
    """Fetch an environment variable, exiting with an error if not set."""
    value = os.getenv(name)
    if value is None:
        print(f"Error: Output variable '{name}' is not set")
        sys.exit(1)
    return value
`
		}
		for _, o := range outputVars {
			if isPsh {
				// If value is empty or null, setting it to empty string and then converting it to base64
				cmd += fmt.Sprintf(
					"\n$envVal = if ($null -eq $Env:%s) { '' } else { $Env:%s }; "+
						"$val = '%s=__B64__' + [Convert]::ToBase64String([System.Text.Encoding]::UTF8.GetBytes($envVal)) \n"+
						"Add-Content -Path %s -Value $val",
					o.Value, o.Value, o.Key, outputFile)
			} else if isPython {
				cmd += fmt.Sprintf(`
try:
    with open('%s', 'a') as out_file:
        value = get_env_var('%s')
        b64val = base64.b64encode(value.encode()).decode()
        out_file.write('%s=__B64__' + b64val + '\n')
except Exception as e:
    print(f"Error: {e}")
    sys.exit(1)
`, outputFile, o.Key, o.Value)
			} else {
				cmd += fmt.Sprintf("\nprintf '%%s=__B64__%%s\\n' '%s' \"$(printf '%%s' \"$%s\" | base64 | tr -d '\\n')\" >> %s",
					o.Key,
					o.Value,
					outputFile)
			}
		}
	} else {
		if isPsh {
			cmd += fmt.Sprintf("\nNew-Item %s", outputFile)
		} else if isPython {
			cmd += "\nimport os\n"
		}
		for _, o := range outputVars {
			if isPsh {
				cmd += fmt.Sprintf("\n$val = \"%s=$Env:%s\" \nAdd-Content -Path %s -Value $val", o.Key, o.Value, outputFile)
			} else if isPython {
				cmd += fmt.Sprintf("with open('%s', 'a') as out_file:\n\tout_file.write('%s=' + os.getenv('%s') + '\\n')\n", outputFile, o.Key, o.Value)
			} else {
				cmd += fmt.Sprintf("\necho \"%s=$%s\" >> %s", o.Key, o.Value, outputFile)
			}
		}
	}

	return cmd
}

func IsPowershell(entrypoint []string) bool {
	if len(entrypoint) > 0 && (entrypoint[0] == "powershell" || entrypoint[0] == "pwsh") {
		return true
	}
	return false
}

func isPython(entrypoint []string) bool {
	if len(entrypoint) > 0 && (entrypoint[0] == "python3") {
		return true
	}
	return false
}

// Fetches variable in env file exported by the step.
func fetchExportedVarsFromEnvFile(envFile string, out io.Writer, useCINewGodotEnvVersion bool) (map[string]string, error) {
	log := logrus.New()
	log.Out = out

	if _, err := os.Stat(envFile); errors.Is(err, os.ErrNotExist) {
		return nil, err
	}

	var (
		env map[string]string
		err error
	)

	if useCINewGodotEnvVersion {
		env, err = v4.Read(envFile)
		if err != nil {
			env, err = v2.Read(envFile)
		}
	} else {
		env, err = v2.Read(envFile)
	}

	if err != nil {
		content, ferr := os.ReadFile(envFile)
		if ferr != nil {
			log.WithError(ferr).WithField("envFile", envFile).Warnln("Unable to read exported env file")
		}
		log.WithError(err).WithField("envFile", envFile).WithField("content", string(content)).Warnln("failed to read exported env file")
		if errors.Is(err, bufio.ErrTooLong) {
			err = fmt.Errorf("output variable length is more than %d bytes", bufio.MaxScanTokenSize)
		}
		return nil, err
	}

	// base64 decode if __B64__ prefix is present
	for k, v := range env {
		if strings.HasPrefix(v, "__B64__") {
			b64Value := v[len("__B64__"):]
			decodedValue, err := b64.StdEncoding.DecodeString(b64Value)
			if err != nil {
				log.WithError(err).Errorln("Failed to decode base64 value")
			} else {
				env[k] = string(decodedValue)
			}
		}
	}

	return env, nil
}

func fetchArtifactDataFromArtifactFile(artifactFile string, out io.Writer) ([]byte, error) {
	log := logrus.New()
	log.Out = out

	if _, err := os.Stat(artifactFile); errors.Is(err, os.ErrNotExist) {
		return nil, err
	}

	content, err := os.ReadFile(artifactFile)
	if err != nil {
		log.WithError(err).WithField("artifactFile", artifactFile).WithField("content", string(content)).Warnln("failed to read artifact file")
		return nil, err
	}
	return content, nil
}

// setTiEnvVariables sets the environment variables required for TI
func setTiEnvVariables(step *spec.Step, config *tiCfg.Cfg) {
	if config == nil {
		return
	}
	if step.Envs == nil {
		step.Envs = map[string]string{}
	}

	envMap := step.Envs
	envMap[ti.TiSvcEp] = config.GetURL()
	envMap[ti.TiSvcToken] = b64.StdEncoding.EncodeToString([]byte(config.GetToken()))
	envMap[ti.AccountIDEnv] = config.GetAccountID()
	envMap[ti.OrgIDEnv] = config.GetOrgID()
	envMap[ti.ProjectIDEnv] = config.GetProjectID()
	envMap[ti.PipelineIDEnv] = config.GetPipelineID()
	envMap[ti.StageIDEnv] = config.GetStageID()
	envMap[ti.BuildIDEnv] = config.GetBuildID()
	envMap[ti.StepIDEnv] = step.Name
	envMap[ti.InfraEnv] = ti.HarnessInfra
}

func getLogServiceClient(cfg api.LogConfig) logstream.Client {
	if cfg.URL != "" {
		return remote.NewHTTPClient(cfg.URL, cfg.AccountID, cfg.Token, cfg.IndirectUpload, false, "", "", "")
	}
	return stdout.New()
}

// Used to create a log service client which handles secrets
// If the URL is not set, it will write to stdout instead.
func GetReplacer(
	cfg api.LogConfig, logKey, name string, secrets []string,
) logstream.Writer {
	client := getLogServiceClient(cfg)
	wc := livelog.New(context.Background(), client, logKey, name, []logstream.Nudge{}, false, cfg.TrimNewLineSuffix, cfg.SkipOpeningStream, cfg.SkipClosingStream)
	return logstream.NewReplacer(wc, secrets)
}

func GetReplacerWithCustomLogClient(
	ctx context.Context, client logstream.Client, cfg api.LogConfig, logKey, name string, secrets []string,
) logstream.Writer {
	wc := livelog.New(ctx, client, logKey, name, []logstream.Nudge{}, false, cfg.TrimNewLineSuffix, cfg.SkipOpeningStream, cfg.SkipClosingStream)
	return logstream.NewReplacer(wc, secrets)
}

//nolint:unused // may be used for future functionality
func waitForZipUnlock(timeout time.Duration, tiConfig *tiCfg.Cfg) error {
	deadline := time.Now().Add(timeout)
	for {
		time.Sleep(time.Second * 1)
		if !tiConfig.IsZipLocked() {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timeout waiting for agent download")
		}
	}
}

// checkStepSuccess checks if the step was successful based on the return values
func checkStepSuccess(state *runtime.State, err error) bool {
	if err == nil && state != nil && state.ExitCode == 0 && state.Exited {
		return true
	}
	return false
}

// injectHcliPathForWindowsContainer adds hcli to PATH for Windows container steps.
func injectHcliPathForWindowsContainer(step *spec.Step) {
	defer func() {
		if r := recover(); r != nil {
			logrus.WithField("panic", r).Warn("recovered from panic in hcli PATH injection")
		}
	}()

	if !shouldInjectHcliPath(step) {
		return
	}

	shell := ""
	if len(step.Entrypoint) > 0 {
		shell = strings.ToLower(step.Entrypoint[0])
	}

	// Prepend hcli path to ensure our binary is found first
	switch {
	case strings.Contains(shell, "powershell"), strings.Contains(shell, "pwsh"):
		step.Command[0] = `$env:PATH = '` + hcliWindowsContainerPath + `;' + $env:PATH; ` + step.Command[0]
	case strings.Contains(shell, "cmd"):
		step.Command[0] = `set "PATH=` + hcliWindowsContainerPath + `;%PATH%" & ` + step.Command[0]
	}
}

// shouldInjectHcliPath checks if hcli PATH injection should be performed.
func shouldInjectHcliPath(step *spec.Step) bool {
	if step == nil || step.Envs == nil {
		return false
	}
	if goruntime.GOOS != "windows" {
		return false
	}
	if step.Image == "" || len(step.Command) == 0 {
		return false
	}
	return step.Envs[annotationsFFEnv] == trueValue
}
