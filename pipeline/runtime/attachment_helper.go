// Copyright 2026 Harness Inc. All rights reserved.
// Use of this source code is governed by the PolyForm Free Trial 1.0.0 license
// that can be found in the licenses directory at the root of this repository, also available at
// https://polyformproject.org/wp-content/uploads/2020/05/PolyForm-Free-Trial-1.0.0.txt.

package runtime

import (
	"context"
	"io"
	"runtime/debug"
	"strings"

	"github.com/harness/lite-engine/api"
	"github.com/harness/lite-engine/ti/attachments"
	"github.com/harness/lite-engine/ti/report/parser/junit"
	"github.com/sirupsen/logrus"
)

// maskHarToken adds the token of the step's registry config, trimmed like the copy that is sent, to the secrets of
// the step, so that it never shows up in the step log or the output of the step. CI Manager sends the config only
// with steps that upload test attachments. The stateless executor gets its log writer from the caller, which masks
// the secrets of the step request itself, so there the token is covered only through the masks CI Manager sends.
func maskHarToken(r *api.StartStepRequest) {
	if tok := strings.TrimSpace(r.HarConfig.Token); tok != "" {
		r.Secrets = append(r.Secrets, tok)
	}
}

// logAttachmentPanic tells the user in the step log that the attachments failed, and keeps the stack trace in the
// lite engine log.
func logAttachmentPanic(log *logrus.Logger, rec any) {
	log.Warnf("Test attachment upload failed: %v. Test results are unaffected.", rec)
	logrus.WithField("panic", rec).WithField("stack", string(debug.Stack())).Errorln("Panic in test attachment upload")
}

// planTestAttachments selects the files of a step whose test attachments are uploaded to the Harness Artifact
// Registry, before the test report is parsed so that the report can link them. It plans only for steps that have a
// JUnit report and set PLUGIN_UPLOAD_ARTIFACTS and that CI Manager sent a registry config for. The registry
// settings come only from that config, never from the step or stage environment, which the user controls. It
// returns nil when there is nothing to upload.
func planTestAttachments(ctx context.Context, r *api.StartStepRequest, out io.Writer) (plan *attachments.Plan) {
	// Detached steps never upload test attachments.
	if r.Detach || len(r.TestReport.Junit.Paths) == 0 {
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
			logAttachmentPanic(log, rec)
		}
	}()
	return attachments.NewPlan(&r.HarConfig, &attachments.Params{
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
	if plan == nil {
		return
	}
	log := logrus.New()
	log.Out = out
	defer func() {
		if rec := recover(); rec != nil {
			logAttachmentPanic(log, rec)
		}
	}()
	if reportErr != nil || !plan.Linked() {
		log.Warnf("Test attachment upload skipped: the test report was not written or links none of them, "+
			"%d files were not uploaded", plan.FileCount())
		return
	}
	plan.Upload(ctx, log)
}
