// Copyright 2022 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package runtime

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/drone/runner-go/pipeline/runtime"
	"github.com/harness/lite-engine/api"
	"github.com/harness/lite-engine/pipeline"
	tiCfg "github.com/harness/lite-engine/ti/config"
	"github.com/harness/lite-engine/ti/report"
	"github.com/harness/lite-engine/ti/savings"
	"github.com/harness/ti-client/types"
	"github.com/sirupsen/logrus"
)

const (
	trueValue = "true"
)

func executeRunStep(ctx context.Context, f RunFunc, r *api.StartStepRequest, out io.Writer, tiConfig *tiCfg.Cfg) ( //nolint:gocritic,gocyclo
	*runtime.State, map[string]string, map[string]string, []byte, []*api.OutputV2, string, error) {
	start := time.Now()
	step := toStep(r)
	step.Command = r.Run.Command
	step.Entrypoint = r.Run.Entrypoint
	setTiEnvVariables(step, tiConfig)

	optimizationState := types.DISABLED
	exportEnvFile := fmt.Sprintf("%s/%s-export.env", pipeline.SharedVolPath, step.ID)
	step.Envs["DRONE_ENV"] = exportEnvFile

	if (len(r.OutputVars) > 0 || len(r.Outputs) > 0) && (len(step.Entrypoint) == 0 || len(step.Command) == 0) {
		return nil, nil, nil, nil, nil, string(optimizationState), fmt.Errorf("output variable should not be set for unset entrypoint or command")
	}

	if r.ScratchDir != "" {
		// Plugins can use this directory as a scratch space to store temporary files.
		// It will get cleaned up after a destroy.
		step.Envs["HARNESS_SCRATCH_DIR"] = r.ScratchDir
	}

	// If the output variable file is set, it means we use the file directly to get the output variables
	// instead of explicitly modifying the input command.
	var outputFile string
	if r.OutputVarFile != "" {
		// Plugins can use HARNESS_OUTPUT_FILE to write the output variables to a file.
		step.Envs["HARNESS_OUTPUT_FILE"] = r.OutputVarFile
		outputFile = r.OutputVarFile
	} else {
		// If output variable file is not set, we auto append the run command to write output
		// variables.
		outputFile = fmt.Sprintf("%s/%s-output.env", pipeline.SharedVolPath, step.ID)
		step.Envs["DRONE_OUTPUT"] = outputFile

		if len(r.Outputs) > 0 {
			step.Command[0] += getOutputsCmd(step.Entrypoint, r.Outputs, outputFile)
		} else if len(r.OutputVars) > 0 {
			step.Command[0] += getOutputVarCmd(step.Entrypoint, r.OutputVars, outputFile)
		}
	}

	var outputSecretsFile string
	if r.SecretVarFile != "" {
		outputSecretsFile = r.SecretVarFile
	} else {
		outputSecretsFile = fmt.Sprintf("%s/%s-output-secrets.env", pipeline.SharedVolPath, step.ID)
	}
	// Plugins can use HARNESS_OUTPUT_SECRET_FILE to write the output secrets to a file.
	step.Envs["HARNESS_OUTPUT_SECRET_FILE"] = outputSecretsFile

	artifactFile := fmt.Sprintf("%s/%s-artifact", pipeline.SharedVolPath, step.ID)
	step.Envs["PLUGIN_ARTIFACT_FILE"] = artifactFile

	if metadataFile, found := step.Envs["PLUGIN_METADATA_FILE"]; found {
		step.Envs["PLUGIN_METADATA_FILE"] = fmt.Sprintf("%s/%s-%s", pipeline.SharedVolPath, step.ID, metadataFile)
	}

	log := logrus.New()
	log.Out = out

	exited, err := f(ctx, step, out, r.LogDrone)
	timeTakenMs := time.Since(start).Milliseconds()

	reportStart := time.Now()
	if rerr := report.ParseAndUploadTests(ctx, r.TestReport, r.WorkingDir, step.Name, log, reportStart, tiConfig, r.Envs); rerr != nil {
		logrus.WithError(rerr).WithField("step", step.Name).Errorln("failed to upload report")
		log.Errorf("Failed to upload report. Time taken: %s", time.Since(reportStart))
	}

	// Parse and upload savings to TI
	if tiConfig.GetParseSavings() {
		optimizationState = savings.ParseAndUploadSavings(ctx, r.WorkingDir, log, step.Name, timeTakenMs, tiConfig)
	}

	useCINewGodotEnvVersion := false
	if val, ok := step.Envs[ciNewVersionGodotEnv]; ok && val == trueValue {
		useCINewGodotEnvVersion = true
	}

	exportEnvs, _ := fetchExportedVarsFromEnvFile(exportEnvFile, out, useCINewGodotEnvVersion)
	artifact, _ := fetchArtifactDataFromArtifactFile(artifactFile, out)
	if exited != nil && exited.Exited && exited.ExitCode == 0 {
		outputs, err := fetchExportedVarsFromEnvFile(outputFile, out, useCINewGodotEnvVersion) //nolint:govet
		if err != nil {
			log.WithError(err).Errorln("error encountered while fetching secrets from env File")
		}
		outputsV2 := []*api.OutputV2{}
		var finalErr error
		if len(r.Outputs) > 0 {
			// only return err when output vars are expected
			finalErr = err
			for _, output := range r.Outputs {
				if _, ok := outputs[output.Key]; ok {
					outputsV2 = append(outputsV2, &api.OutputV2{
						Key:   output.Key,
						Value: outputs[output.Key],
						Type:  output.Type,
					})
				}
			}
		} else {
			if len(r.OutputVars) > 0 {
				// only return err when output vars are expected
				finalErr = err
			}
			for key, value := range outputs {
				output := &api.OutputV2{
					Key:   key,
					Value: value,
					Type:  api.OutputTypeString,
				}
				outputsV2 = append(outputsV2, output)
			}
		}

		//checking exported secrets from plugins if any
		if _, err := os.Stat(outputSecretsFile); err == nil {
			secrets, err := fetchExportedVarsFromEnvFile(outputSecretsFile, out, useCINewGodotEnvVersion)
			if err != nil {
				log.WithError(err).Errorln("error encountered while fetching output secrets from env File")
			}
			for key, value := range secrets {
				output := &api.OutputV2{
					Key:   key,
					Value: value,
					Type:  api.OutputTypeSecret,
				}
				outputsV2 = append(outputsV2, output)
			}

		}

		return exited, outputs, exportEnvs, artifact, outputsV2, string(optimizationState), finalErr
	}
	return exited, nil, exportEnvs, artifact, nil, string(optimizationState), err
}
