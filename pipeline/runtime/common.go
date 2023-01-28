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
	"strings"

	"github.com/harness/lite-engine/engine/spec"
	"github.com/harness/lite-engine/logstream"
	"github.com/harness/lite-engine/pipeline"
	"github.com/harness/lite-engine/ti"
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
		cmd += "import os\n"
	}
	for _, o := range outputVars {
		if isPsh {
			cmd += fmt.Sprintf("\n$val = \"%s $Env:%s\" \nAdd-Content -Path %s -Value $val", o, o, outputFile)
		} else if isBash(entrypoint) || isSh(entrypoint) {
			cmd += fmt.Sprintf(";echo \"%s $%s\" >> %s", o, o, outputFile)
		} else if isPython {
			cmd = cmd + fmt.Sprintf("with open(%s, 'a') as out_file:\n\tout_file.write(\"%s {}\n\".format(os.getenv(%s)))\n", outputFile, o, o)
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

func isSh(entrypoint []string) bool {
	if len(entrypoint) > 0 && (entrypoint[0] == "sh") {
		return true
	}
	return false
}

func isBash(entrypoint []string) bool {
	if len(entrypoint) > 0 && (entrypoint[0] == "bash") {
		return true
	}
	return false
}

func isPython(entrypoint []string) bool {
	if len(entrypoint) > 0 && (entrypoint[0] == "python") {
		return true
	}
	return false
}

// Fetches map of env variable and value from OutputFile.
// OutputFile stores all env variable and value
func fetchOutputVariables(outputFile string, out io.Writer) (map[string]string, error) {
	log := logrus.New()
	log.Out = out

	outputs := make(map[string]string)
	f, err := os.Open(outputFile)
	if err != nil {
		log.WithError(err).WithField("outputFile", outputFile).Errorln("failed to open output file")
		return nil, err
	}
	defer f.Close()

	s := bufio.NewScanner(f)
	for s.Scan() {
		line := s.Text()
		sa := strings.Split(line, " ")
		if len(sa) < 2 { //nolint:gomnd
			log.WithField("variable", sa[0]).Warnln("output variable does not exist")
		} else {
			outputs[sa[0]] = line[len(sa[0])+1:]
		}
	}
	if err := s.Err(); err != nil {
		log.WithError(err).Errorln("failed to create scanner from output file")
		return nil, err
	}
	return outputs, nil
}

// Fetches env variable exported by the step.
func fetchExportedEnvVars(envFile string, out io.Writer) map[string]string {
	log := logrus.New()
	log.Out = out

	if _, err := os.Stat(envFile); errors.Is(err, os.ErrNotExist) {
		return nil
	}

	env, err := godotenv.Read(envFile)
	if err != nil {
		log.WithError(err).WithField("envFile", envFile).Warnln("failed to read exported env file")
		return nil
	}
	return env
}

// setTiEnvVariables sets the environment variables required for TI
func setTiEnvVariables(step *spec.Step) {
	config := pipeline.GetState().GetTIConfig()
	if config == nil {
		return
	}
	if step.Envs == nil {
		step.Envs = map[string]string{}
	}

	envMap := step.Envs
	envMap[ti.TiSvcEp] = config.URL
	envMap[ti.TiSvcToken] = b64.StdEncoding.EncodeToString([]byte(config.Token))
	envMap[ti.AccountIDEnv] = config.AccountID
	envMap[ti.OrgIDEnv] = config.OrgID
	envMap[ti.ProjectIDEnv] = config.ProjectID
	envMap[ti.PipelineIDEnv] = config.PipelineID
	envMap[ti.InfraEnv] = ti.HarnessInfra
}
