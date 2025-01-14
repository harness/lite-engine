// Copyright 2022 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package runtime

import (
	"bufio"
	b64 "encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/drone/runner-go/pipeline/runtime"
	v2 "github.com/harness/godotenv/v2"
	v3 "github.com/harness/godotenv/v3"
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

func getOutputVarCmd(entrypoint, outputVars []string, outputFile string) string {
	isPsh := IsPowershell(entrypoint)
	isPython := isPython(entrypoint)

	cmd := ""
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
			cmd += fmt.Sprintf("\ntrap 'echo \"%s=$%s\" >> %s' EXIT", o, o, outputFile)
		}
	}

	return cmd
}

func getOutputsCmd(entrypoint []string, outputVars []*api.OutputV2, outputFile string) string {
	isPsh := IsPowershell(entrypoint)
	isPython := isPython(entrypoint)

	cmd := ""
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
			cmd += fmt.Sprintf("\ntrap 'echo \"%s='$%s'\" >> %s' EXIT", o.Key, o.Value, outputFile)
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
		env, err = v3.Read(envFile)
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
		return remote.NewHTTPClient(cfg.URL, cfg.AccountID, cfg.Token, cfg.IndirectUpload, false, "", "")
	}
	return stdout.New()
}

// Used to create a log service client which handles secrets
// If the URL is not set, it will write to stdout instead.
func GetReplacer(
	cfg api.LogConfig, logKey, name string, secrets []string,
) logstream.Writer {
	client := getLogServiceClient(cfg)
	wc := livelog.New(client, logKey, name, []logstream.Nudge{}, false, cfg.TrimNewLineSuffix, cfg.SkipOpeningStream)
	return logstream.NewReplacer(wc, secrets)
}

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
