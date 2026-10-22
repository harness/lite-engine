// Copyright 2026 Harness Inc. All rights reserved.
// Use of this source code is governed by the PolyForm Free Trial 1.0.0 license
// that can be found in the licenses directory at the root of this repository, also available at
// https://polyformproject.org/wp-content/uploads/2020/05/PolyForm-Free-Trial-1.0.0.txt.

package runtime

import (
	"context"
	"io"

	"github.com/harness/lite-engine/api"
	"github.com/harness/lite-engine/ti/attachments"
	"github.com/sirupsen/logrus"
)

// uploadTestAttachments uploads the test attachments of a finished step to the Harness Artifact Registry.
// It runs only for steps that have a JUnit report and set PLUGIN_UPLOAD_ARTIFACTS, and never changes the
// step result: failures are written to the step log. The registry settings and workspace come only from the
// config CI Manager sent at setup, never from the step or stage environment, which the user controls.
// It reports whether an upload was attempted.
func uploadTestAttachments(ctx context.Context, r *api.StartStepRequest, harCfg *api.HarConfig, out io.Writer) bool {
	if harCfg == nil || out == nil || len(r.TestReport.Junit.Paths) == 0 || ctx.Err() != nil {
		return false
	}
	mode, optedIn := r.Envs[attachments.UploadArtifactsEnv]
	if !optedIn {
		return false
	}

	p := &attachments.Params{
		StepID:      r.ID,
		WorkingDir:  r.WorkingDir,
		ReportPaths: r.TestReport.Junit.Paths,
		Mode:        mode,
		ProjectRoot: r.Envs[attachments.ProjectRootEnv],
	}
	cfg := &attachments.Config{
		URL:              harCfg.URL,
		AccountID:        harCfg.AccountID,
		Registry:         harCfg.Registry,
		PipelineUniqueID: harCfg.PipelineUniqueID,
		BuildID:          harCfg.BuildID,
		Token:            harCfg.Token,
		Workspace:        harCfg.Workspace,
	}
	log := logrus.New()
	log.Out = out
	// The step context bounds the upload together with the upload's own time limit.
	attachments.Upload(ctx, cfg, p, log)
	return true
}
