// Copyright 2022 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package runtime

import (
	b64 "encoding/base64"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"os"

	"github.com/harness/lite-engine/engine/spec"
	"github.com/harness/lite-engine/logstream"
	"github.com/harness/lite-engine/ti"
	tiCfg "github.com/harness/lite-engine/ti/config"
	"github.com/joho/godotenv"
	"github.com/sirupsen/logrus"
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
	isPsh := isPowershell(entrypoint)
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
			cmd += fmt.Sprintf("\necho \"%s=$%s\" >> %s", o, o, outputFile)
		}
	}

	return cmd
}

func isPowershell(entrypoint []string) bool {
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
func fetchExportedVarsFromEnvFile(envFile string, out io.Writer) (map[string]string, error) {
	log := logrus.New()
	log.Out = out

	if _, err := os.Stat(envFile); errors.Is(err, os.ErrNotExist) {
		return nil, err
	}

	env, err := godotenv.Read(envFile)
	if err != nil {
		content, ferr := os.ReadFile(envFile)
		if ferr != nil {
			log.WithError(ferr).WithField("envFile", envFile).Warnln("Unable to read exported env file")
		}
		log.WithError(err).WithField("envFile", envFile).WithField("content", string(content)).Warnln("failed to read exported env file")
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

	contents, _ := ioutil.ReadFile(artifactFile)
	log.Info("Artifact contents...")
	log.Info(string(contents))

	var content []byte
	if content, err := os.ReadFile(artifactFile); err != nil { //nolint:govet
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
	envMap[ti.InfraEnv] = ti.HarnessInfra
}
