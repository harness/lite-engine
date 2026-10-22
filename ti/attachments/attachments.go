// Copyright 2026 Harness Inc. All rights reserved.
// Use of this source code is governed by the PolyForm Free Trial 1.0.0 license
// that can be found in the licenses directory at the root of this repository, also available at
// https://polyformproject.org/wp-content/uploads/2020/05/PolyForm-Free-Trial-1.0.0.txt.

package attachments

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/harness/ti-client/types"
	"github.com/mattn/go-zglob"
	"github.com/sirupsen/logrus"
)

const (
	// UploadArtifactsEnv opts a step in: "all" (also the default for an empty value) uploads every test,
	// "failed" only failed or retried tests, "none" disables the upload.
	UploadArtifactsEnv = "PLUGIN_UPLOAD_ARTIFACTS"
	// uploadArtifactsSetting is the step setting users write, which reaches the step as UploadArtifactsEnv.
	uploadArtifactsSetting = "upload_artifacts"
	// ProjectRootEnv restricts the attachments to a directory below the workspace.
	ProjectRootEnv = "PLUGIN_PROJECT_ROOT"

	bytesPerMiB = 1 << 20
	// attemptIDBytes is the number of random bytes in an attempt id: 16 hex characters.
	attemptIDBytes = 8
)

// With less step time left than minUploadTime plus stepEndReserve the upload is skipped: it would mostly be cut
// off. The upload ends stepEndReserve before the step deadline, which leaves the step time to finish. Variables
// so that tests do not wait minutes.
var (
	minUploadTime  = time.Minute
	stepEndReserve = time.Minute
)

// Params describe one step whose test attachments may be uploaded.
type Params struct {
	// StepID keeps the files of parallel and matrix steps of the same build apart.
	StepID string
	// WorkingDir is the step working directory, which relative report paths are resolved against.
	WorkingDir string
	// ReportPaths are the JUnit report paths of the step.
	ReportPaths []string
	// Mode is the value of PLUGIN_UPLOAD_ARTIFACTS. The caller checks that the step opted in.
	Mode string
	// ProjectRoot is the value of PLUGIN_PROJECT_ROOT.
	ProjectRoot string
	// Deadline is the end of the step timeout. The zero value means the step has no timeout.
	Deadline time.Time
}

// Plan holds the files of one run of a step and the links the test report gets for them. The links are written
// to the test report before the files are uploaded, like in the Kubernetes addon.
type Plan struct {
	cfg       Config
	stepID    string
	attemptID string
	root      string
	files     []file
	// byDir maps every directory above a planned file, relative to the project root, to the files below it.
	byDir map[string][]types.TestArtifact
	// annotated holds the tests that already got artifacts, see Annotate.
	annotated map[string]bool
}

// NewPlan selects the result directories of the tests that the JUnit reports reference with [[ATTACHMENT|path]]
// lines, without uploading anything. Only Playwright traces and videos are selected, see testArtifactType.
// PLUGIN_UPLOAD_ARTIFACTS selects the tests: "all" (also the default for an empty value) plans every test,
// "failed" only failed or retried tests, "none" disables the feature. It returns nil, and the test report stays
// unchanged, when the registry is not configured for the step, when there is nothing to upload, or when too little
// time is left until the step deadline. Failures are logged and never change the step result.
func NewPlan(cfg *Config, p *Params, log *logrus.Logger) *Plan {
	if len(p.ReportPaths) == 0 {
		return nil
	}
	mode, valid := normalizeMode(p.Mode)
	if valid && mode == "none" {
		return nil
	}

	// The registry configuration is set by CI Manager only for steps that upload, so check it before parsing
	// reports or walking result directories. Without it the step log stays unchanged.
	checked, err := checkConfig(cfg)
	if err != nil {
		if errors.Is(err, ErrIncomplete) {
			log.WithError(err).Warnln("Test attachment upload skipped")
		} else {
			log.Debugln("Test attachment upload skipped: not enabled for this step")
		}
		return nil
	}
	if !validStepID(p.StepID) {
		log.Warnf("Test attachment upload skipped: invalid step id %q", p.StepID)
		return nil
	}
	var files []file
	var skipped []string
	root := resolveProjectRoot(p.ProjectRoot, p.WorkingDir, checked.Workspace)
	if valid {
		if err := checkProjectRoot(root, checked.Workspace); err != nil {
			log.Warnf("Test attachment upload skipped: %v", err)
			return nil
		}
		files, skipped = selectFiles(root, junitFiles(p.ReportPaths, p.WorkingDir), mode == "all")
	} else {
		skipped = []string{fmt.Sprintf("invalid %s (%s) value %q, nothing is uploaded (use all, failed or none)",
			uploadArtifactsSetting, UploadArtifactsEnv, mode)}
	}
	for _, s := range skipped {
		log.Warnf("Test attachment upload: %s", s)
	}
	if len(files) == 0 {
		log.Debugf("Test attachment upload: no attachments to upload (mode=%s)", mode)
		return nil
	}
	if !hasUploadTime(p.Deadline) {
		warnStepTimeout(log, len(files), false)
		return nil
	}

	var totalBytes int64
	for _, f := range files {
		totalBytes += f.size
	}
	log.Debugf("Uploading %d test attachment files (%.1f MiB, mode=%s) to Harness after the test report",
		len(files), float64(totalBytes)/bytesPerMiB, mode)
	return newPlan(&checked, p.StepID, newAttemptID(), root, files)
}

// newPlan indexes the planned files for the test report.
func newPlan(cfg *Config, stepID, attemptID, root string, files []file) *Plan {
	p := &Plan{cfg: *cfg, stepID: stepID, attemptID: attemptID, root: root, files: files,
		byDir: map[string][]types.TestArtifact{}, annotated: map[string]bool{}}
	for _, f := range files {
		dest := f.destination
		artifact := types.TestArtifact{
			Type: testArtifactType(dest),
			// <step id>/<attempt>/<result dir>/<file>: ti-service takes the pipeline and the build from the request.
			Path:    stepID + "/" + attemptID + "/" + dest,
			Size:    f.size,
			Attempt: retryAttempt(dest),
		}
		for dir := path.Dir(dest); dir != "." && dir != "/"; dir = path.Dir(dir) {
			p.byDir[dir] = append(p.byDir[dir], artifact)
		}
	}
	return p
}

// Linked reports whether the test report links at least one planned file. Files no test links are not uploaded.
func (p *Plan) Linked() bool { return p != nil && len(p.annotated) > 0 }

// FileCount is the number of files planned for upload.
func (p *Plan) FileCount() int {
	if p == nil {
		return 0
	}
	return len(p.files)
}

// hasUploadTime reports whether the step has time left to upload the files the test report links. A step without
// a timeout has none, like in Kubernetes.
func hasUploadTime(deadline time.Time) bool {
	return !deadline.IsZero() && time.Until(deadline) >= minUploadTime+stepEndReserve
}

// warnStepTimeout tells the user that the step timeout left files without an upload, and how to change that. reached
// is false when the upload did not start because too little step time was left.
func warnStepTimeout(log *logrus.Logger, notUploaded int, reached bool) {
	reason := "too little step time was left"
	if reached {
		reason = "the step timeout was reached"
	}
	log.Warnf("Test attachment upload: %d files were not uploaded because %s. "+
		"Increase the step timeout to upload them, or set %s (%s) to failed or none.", notUploaded, reason,
		uploadArtifactsSetting, UploadArtifactsEnv)
}

// newAttemptID names the directory below the step id that holds the files of one run of the step. A step that runs
// again keeps its step id, for example under a retry strategy, and the registry does not let the upload token replace
// a file. Without the attempt directory the files of the second run would be refused and its tests linked to the files
// of the first run.
func newAttemptID() string {
	b := make([]byte, attemptIDBytes)
	_, _ = rand.Read(b) // never fails, since Go 1.24
	return hex.EncodeToString(b)
}

// Upload sends the planned files to the registry, after the test report that links them was written. It ends
// stepEndReserve before deadline, and it is skipped without hasUploadTime. Failures are logged and never change
// the step result.
func (p *Plan) Upload(ctx context.Context, deadline time.Time, log *logrus.Logger) {
	if p == nil {
		return
	}
	if !hasUploadTime(deadline) {
		warnStepTimeout(log, len(p.files), false)
		return
	}
	// Only the step timeout bounds the upload, like in Kubernetes.
	ctx, cancel := context.WithDeadline(ctx, deadline.Add(-stepEndReserve))
	defer cancel()

	start := time.Now()
	// <pipeline>/v<build>/test-attachments/<step id>/<attempt>/<destination>, like the lite engine of Kubernetes.
	uploadRoot := "/" + path.Join(p.cfg.PipelineUniqueID, "v"+p.cfg.BuildID, destinationDir, p.stepID)
	files := make([]file, len(p.files))
	for i, f := range p.files {
		f.destination = p.attemptID + "/" + f.destination
		files[i] = f
	}
	uploads, invalid := resolveUploads(p.cfg.Workspace, uploadRoot, files)
	failures := collectErrors(nil, invalid)
	uploaded, timedOut := 0, 0
	for _, err := range uploadAll(ctx, &p.cfg, uploads) {
		switch {
		case err == nil:
			uploaded++
		case ctx.Err() != nil && errors.Is(err, ctx.Err()):
			// Counted below, not logged one by one.
			timedOut++
		default:
			failures = collectErrors(failures, []error{err})
		}
	}
	for _, e := range failures {
		log.Warnf("Test attachment upload failed for %s", e)
	}
	log.Infof("Test attachment upload finished in %s: %d uploaded, %d not confirmed as uploaded",
		time.Since(start).Round(time.Millisecond), uploaded, len(p.files)-uploaded)
	if timedOut > 0 {
		warnStepTimeout(log, timedOut, true)
	}
}

// Annotate sets TestCase.Artifacts from the files planned for upload from the result directories named by the
// [[ATTACHMENT|path]] lines in the system-out of the test case. systemOut is the full text, as the copy in the test
// case is truncated. A test that appears again with the same result directories, for example when several JUnit
// reports list it, gets no second copy of the artifacts. Like the file selection, a line counts only when it names
// a regular file below the project root. It is safe to call on a nil Plan.
func (p *Plan) Annotate(junitFile, systemOut string, tc *types.TestCase) {
	if p == nil {
		return
	}
	markers := attachmentPattern.FindAllStringSubmatch(systemOut, -1)
	if len(markers) == 0 {
		return
	}
	prefixSet := map[string]bool{}
	for _, m := range markers {
		_, dir, ok := resolveAttachment(p.root, junitFile, strings.TrimSpace(m[1]))
		if !ok || dir == p.root {
			continue
		}
		if rel, err := filepath.Rel(p.root, dir); err == nil {
			prefixSet[filepath.ToSlash(rel)] = true
		}
	}
	if len(prefixSet) == 0 {
		return
	}
	prefixes := make([]string, 0, len(prefixSet))
	for prefix := range prefixSet {
		prefixes = append(prefixes, prefix)
	}
	sort.Strings(prefixes)

	key := strings.Join([]string{tc.SuiteName, tc.ClassName, tc.Name, tc.FileName, strings.Join(prefixes, "|")}, "\x00")
	if p.annotated[key] {
		return
	}
	artifacts := p.artifactsUnder(prefixes)
	if len(artifacts) == 0 {
		return
	}
	p.annotated[key] = true
	tc.Artifacts = artifacts
}

// artifactsUnder returns the planned traces and videos in the given directories or below them, relative to the
// project root. Linking is per directory, like the file selection, so tests whose markers name the same directory all
// get its files.
func (p *Plan) artifactsUnder(dirs []string) []types.TestArtifact {
	var artifacts []types.TestArtifact
	// A file below two of the directories, for example login and login/attachments, is listed once.
	seen := map[string]bool{}
	for _, d := range dirs {
		for _, a := range p.byDir[d] {
			if !seen[a.Path] {
				seen[a.Path] = true
				artifacts = append(artifacts, a)
			}
		}
	}
	sort.Slice(artifacts, func(i, j int) bool {
		if artifacts[i].Attempt != artifacts[j].Attempt {
			return artifacts[i].Attempt < artifacts[j].Attempt
		}
		return artifacts[i].Path < artifacts[j].Path
	})
	return artifacts
}

// collectErrors appends the messages of errs, capped so that a large failing upload does not flood the log.
func collectErrors(out []string, errs []error) []string {
	for _, err := range errs {
		switch n := len(out); {
		case n < maxReportedErrs:
			out = append(out, err.Error())
		case n == maxReportedErrs:
			out = append(out, errorsOmitted)
		}
	}
	return out
}

func validStepID(id string) bool {
	return id != "" && id == filepath.Base(id) && id != "." && id != ".." && !strings.ContainsAny(id, `/\`)
}

// normalizeMode lowercases the PLUGIN_UPLOAD_ARTIFACTS value and applies the default: an empty value uploads
// all tests. The returned mode is the lowercased, trimmed input, which is what the caller reports when it is
// not valid.
func normalizeMode(value string) (string, bool) {
	mode := strings.ToLower(strings.TrimSpace(value))
	switch mode {
	case "":
		return "all", true
	case "all", "failed", "none":
		return mode, true
	default:
		return mode, false
	}
}

// resolveProjectRoot returns the directory attachments must be inside. Without PLUGIN_PROJECT_ROOT it is the
// workspace. A relative value is relative to the step working directory.
func resolveProjectRoot(projectRoot, workingDir, workspace string) string {
	if projectRoot != "" {
		return resolveAbs(workingDir, projectRoot)
	}
	return filepath.Clean(workspace)
}

// checkProjectRoot rejects a project root that is not the workspace or inside it. The step sets the project root,
// and selection walks it before uploads are checked against the workspace, so an outside root would let the step
// list host directories through the warnings in its log.
func checkProjectRoot(root, workspace string) error {
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return fmt.Errorf("the project root %s cannot be read: %w", root, err)
	}
	realWorkspace, err := filepath.EvalSymlinks(workspace)
	if err != nil {
		return fmt.Errorf("workspace %s: %w", workspace, err)
	}
	if realRoot != realWorkspace && !isWithin(realWorkspace, realRoot) {
		return fmt.Errorf("the project root %s is not inside the workspace %s", root, workspace)
	}
	return nil
}

// junitFiles expands the report paths, which are relative to the working directory unless absolute.
func junitFiles(reportPaths []string, workingDir string) []string {
	var out []string
	seen := map[string]bool{}
	for _, p := range reportPaths {
		if p == "" {
			continue
		}
		p, err := expandTilde(p)
		if err != nil {
			continue
		}
		if !filepath.IsAbs(p) && !strings.HasPrefix(p, "/") && !strings.HasPrefix(p, `\`) {
			p = filepath.Join(workingDir, p)
		}
		matches, err := zglob.Glob(p)
		if err != nil {
			continue
		}
		for _, m := range matches {
			if !seen[m] {
				seen[m] = true
				out = append(out, m)
			}
		}
	}
	return out
}

func expandTilde(p string) (string, error) {
	if p != "~" && !strings.HasPrefix(p, "~/") && !strings.HasPrefix(p, `~\`) {
		return p, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, p[1:]), nil
}
