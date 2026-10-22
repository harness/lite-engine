// Copyright 2026 Harness Inc. All rights reserved.
// Use of this source code is governed by the PolyForm Free Trial 1.0.0 license
// that can be found in the licenses directory at the root of this repository, also available at
// https://polyformproject.org/wp-content/uploads/2020/05/PolyForm-Free-Trial-1.0.0.txt.

package runtime

import (
	"context"
	"io"
	"slices"

	"github.com/harness/lite-engine/api"
	"github.com/harness/lite-engine/ti/attachments"
	"github.com/harness/lite-engine/ti/report/parser/junit"
	"github.com/sirupsen/logrus"
)

// maskHarToken adds the token of the step's registry config to the secrets of the step, so that it never shows
// up in the step log or the output of the step. CI Manager sends the config only with steps that upload test
// attachments. The stateless executor gets its log writer from the caller, which masks the secrets of the step
// request itself, so there the token is covered only through the masks CI Manager sends.
func maskHarToken(r *api.StartStepRequest) {
	if tok := r.HarConfig.Token; tok != "" && !slices.Contains(r.Secrets, tok) {
		r.Secrets = append(r.Secrets, tok)
	}
}

// planTestAttachments selects the files of a step whose test attachments are uploaded to the Harness Artifact
// Registry, before the test report is parsed so that the report can link them. It plans only for steps that have a
// JUnit report and set PLUGIN_UPLOAD_ARTIFACTS and that CI Manager sent a registry config for. The registry
// settings come only from that config, never from the step or stage environment, which the user controls. It
// returns nil when there is nothing to upload.
func planTestAttachments(ctx context.Context, r *api.StartStepRequest, out io.Writer) (plan *attachments.Plan) {
	// A detached step runs on after this point, so it has no finished test report.
	if out == nil || r.Detach || len(r.TestReport.Junit.Paths) == 0 {
		return nil
	}
	mode, optedIn := r.Envs[attachments.UploadArtifactsEnv]
	if !optedIn {
		return nil
	}
	deadline, _ := ctx.Deadline()
	log := logrus.New()
	log.Out = out
	// Nothing about the attachments may fail the step or leave it without a result.
	defer func() {
		if rec := recover(); rec != nil {
			plan = nil
			log.Warnf("Test attachment upload failed: %v. Test results are unaffected.", rec)
		}
	}()
	cfg := &attachments.Config{
		URL:              r.HarConfig.URL,
		AccountID:        r.HarConfig.AccountID,
		Registry:         r.HarConfig.Registry,
		PipelineUniqueID: r.HarConfig.PipelineUniqueID,
		BuildID:          r.HarConfig.BuildID,
		Token:            r.HarConfig.Token,
		Workspace:        r.HarConfig.Workspace,
	}
	return attachments.NewPlan(cfg, &attachments.Params{
		StepID:      r.ID,
		WorkingDir:  r.WorkingDir,
		ReportPaths: r.TestReport.Junit.Paths,
		Mode:        mode,
		ProjectRoot: r.Envs[attachments.ProjectRootEnv],
		Deadline:    deadline,
	}, log)
}

// attachmentAnnotator returns the annotator that links the planned files in the test report, or nil without a plan.
func attachmentAnnotator(plan *attachments.Plan) junit.TestAnnotator {
	if plan == nil {
		return nil
	}
	return plan.Annotate
}

// uploadTestAttachments uploads the planned files after the test report that links them was written. Failures are
// written to the step log and never change the step result. The upload ends before the step timeout, so it never
// turns a finished step into a timed-out one. Like in Kubernetes, nothing is uploaded when the report was not
// written or links none of the files: reportErr is the error of writing the report.
func uploadTestAttachments(ctx context.Context, plan *attachments.Plan, reportErr error, out io.Writer) {
	if plan == nil || out == nil {
		return
	}
	deadline, _ := ctx.Deadline()
	log := logrus.New()
	log.Out = out
	defer func() {
		if rec := recover(); rec != nil {
			log.Warnf("Test attachment upload failed: %v. Test results are unaffected.", rec)
		}
	}()
	if reportErr != nil || !plan.Linked() {
		log.Warnf("Test attachment upload skipped: the test report was not written or links none of them, "+
			"%d files were not uploaded", plan.FileCount())
		return
	}
	plan.Upload(ctx, deadline, log)
}
