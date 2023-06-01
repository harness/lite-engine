// Copyright 2022 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package runtime

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/drone/runner-go/pipeline/runtime"
	"github.com/sirupsen/logrus"

	"github.com/harness/lite-engine/api"
	"github.com/harness/lite-engine/engine"
	"github.com/harness/lite-engine/pipeline"
	tiCfg "github.com/harness/lite-engine/ti/config"
	"github.com/harness/lite-engine/ti/report"
)

func executeRunStep(ctx context.Context, engine *engine.Engine, r *api.StartStepRequest, out io.Writer, tiConfig *tiCfg.Cfg) ( //nolint:gocritic
	*runtime.State, map[string]string, map[string]string, []byte, error) {
	step := toStep(r)
	// keep copy
	commandNotSplit := r.Run.Command
	if !r.LogDrone {
		commands := splitCommands(r.Run.Command[0])
		filteredCommands := make([]string, 0)
		for _, command := range commands {
			trimmedCommand := strings.TrimSpace(command)
			if trimmedCommand != "set -xe" {
				filteredCommands = append(filteredCommands, trimmedCommand)
			}
		}
		step.Command = filteredCommands
	} else {
		step.Command = r.Run.Command
	}
	step.Entrypoint = r.Run.Entrypoint
	setTiEnvVariables(step, tiConfig)

	exportEnvFile := fmt.Sprintf("%s/%s-export.env", pipeline.SharedVolPath, step.ID)
	step.Envs["DRONE_ENV"] = exportEnvFile

	if len(r.OutputVars) > 0 && (len(step.Entrypoint) == 0 || len(step.Command) == 0) {
		return nil, nil, nil, nil, fmt.Errorf("output variable should not be set for unset entrypoint or command")
	}

	outputFile := fmt.Sprintf("%s/%s-output.env", pipeline.SharedVolPath, step.ID)
	step.Envs["DRONE_OUTPUT"] = outputFile

	if len(r.OutputVars) > 0 {
		commandNotSplit[0] += getOutputVarCmd(step.Entrypoint, r.OutputVars, outputFile)
	}

	artifactFile := fmt.Sprintf("%s/%s-artifact", pipeline.SharedVolPath, step.ID)
	step.Envs["PLUGIN_ARTIFACT_FILE"] = artifactFile

	log := logrus.New()
	log.Out = out

	exited, err := engine.Run(ctx, step, out)
	if rerr := report.ParseAndUploadTests(ctx, r.TestReport, r.WorkingDir, step.Name, log, time.Now(), tiConfig); rerr != nil {
		logrus.WithError(rerr).WithField("step", step.Name).Errorln("failed to upload report")
	}

	exportEnvs, _ := fetchExportedVarsFromEnvFile(exportEnvFile, out)
	artifact, _ := fetchArtifactDataFromArtifactFile(artifactFile, out)
	if exited != nil && exited.Exited && exited.ExitCode == 0 {
		outputs, err := fetchExportedVarsFromEnvFile(outputFile, out) //nolint:govet
		if len(r.OutputVars) > 0 {
			// only return err when output vars are expected
			return exited, outputs, exportEnvs, artifact, err
		}
		return exited, outputs, exportEnvs, artifact, nil
	}
	return exited, nil, exportEnvs, artifact, err
}

func splitCommands(command string) []string {
	var commands []string

	var buf strings.Builder
	inSingleQuote := false
	inDoubleQuote := false
	prevChar := rune(0)
	for _, ch := range command {
		if ch == '\'' && prevChar != '\\' {
			inSingleQuote = !inSingleQuote
		} else if ch == '"' && prevChar != '\\' {
			inDoubleQuote = !inDoubleQuote
		}

		if !inSingleQuote && !inDoubleQuote && (ch == ';' || ch == '\n') {
			if buf.Len() > 0 {
				// Trim the resulting command before appending
				commands = append(commands, strings.TrimSpace(buf.String()))
				buf.Reset()
			}
		} else {
			buf.WriteRune(ch)
		}

		prevChar = ch
	}

	if buf.Len() > 0 {
		// Trim the resulting command before appending
		commands = append(commands, strings.TrimSpace(buf.String()))
	}

	return commands
}
