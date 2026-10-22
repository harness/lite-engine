// Copyright 2026 Harness Inc. All rights reserved.
// Use of this source code is governed by the PolyForm Free Trial 1.0.0 license
// that can be found in the licenses directory at the root of this repository, also available at
// https://polyformproject.org/wp-content/uploads/2020/05/PolyForm-Free-Trial-1.0.0.txt.

package attachments

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/mattn/go-zglob"
	"github.com/sirupsen/logrus"
)

const (
	// UploadArtifactsEnv opts a step in: "all" (also the default for an empty value) uploads every test,
	// "failed" only failed or retried tests, "none" disables the upload.
	UploadArtifactsEnv = "PLUGIN_UPLOAD_ARTIFACTS"
	// ProjectRootEnv restricts the attachments to a directory below the workspace.
	ProjectRootEnv = "PLUGIN_PROJECT_ROOT"

	uploadTimeout = 15 * time.Minute
	bytesPerMiB   = 1 << 20
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
}

// Upload uploads the result directories of tests that the JUnit reports reference with [[ATTACHMENT|path]]
// lines to the Harness Artifact Registry. It does nothing unless the config is complete. Failures are logged
// and never change the step result.
func Upload(ctx context.Context, cfg *Config, p *Params, log *logrus.Logger) {
	if len(p.ReportPaths) == 0 {
		return
	}
	mode, valid := normalizeMode(p.Mode)
	if valid && mode == "none" {
		return
	}

	// The registry configuration is set by CI Manager, so ask for it before parsing reports or walking
	// result directories. When native artifact storage is off the step log stays unchanged.
	checked, err := checkConfig(cfg)
	if err != nil {
		if errors.Is(err, ErrIncomplete) {
			log.WithError(err).Warnln("Test attachment upload skipped")
		} else {
			log.Debugln("Test attachment upload skipped: not enabled for this stage")
		}
		return
	}
	if !validStepID(p.StepID) {
		log.Warnf("Test attachment upload skipped: invalid step id %q", p.StepID)
		return
	}
	var files []file
	var skipped []string
	if valid {
		root := resolveProjectRoot(p.ProjectRoot, p.WorkingDir, checked.Workspace)
		if err := checkProjectRoot(root, checked.Workspace); err != nil {
			log.Warnf("Test attachment upload skipped: invalid %s: %v", ProjectRootEnv, err)
			return
		}
		files, skipped = selectFiles(root, junitFiles(p.ReportPaths, p.WorkingDir), mode == "all")
	} else {
		skipped = []string{fmt.Sprintf("invalid %s value %q, nothing is uploaded (use all, failed or none)",
			UploadArtifactsEnv, mode)}
	}
	for _, s := range skipped {
		log.Warnf("Test attachment upload: %s", s)
	}
	if len(files) == 0 {
		log.Debugf("Test attachment upload: no attachments to upload (mode=%s)", mode)
		return
	}
	transfer(ctx, &checked, p.StepID, files, mode, log)
}

// transfer uploads the selected files under <pipeline>/v<build>/test-attachments/<step id>/ and logs the result.
func transfer(ctx context.Context, cfg *Config, stepID string, files []file, mode string, log *logrus.Logger) {
	var totalBytes int64
	for _, f := range files {
		totalBytes += f.size
	}
	log.Infof("Uploading %d test attachment files (%.1f MiB, mode=%s) to Harness",
		len(files), float64(totalBytes)/bytesPerMiB, mode)

	ctx, cancel := context.WithTimeout(ctx, uploadTimeout)
	defer cancel()
	start := time.Now()
	uploadRoot := "/" + path.Join(cfg.PipelineUniqueID, "v"+cfg.BuildID, destinationDir, stepID)
	uploads, invalid := resolveUploads(cfg.Workspace, uploadRoot, files)
	failures := collectErrors(nil, invalid)
	uploaded := 0
	for _, err := range uploadAll(ctx, cfg, uploads) {
		if err != nil {
			failures = collectErrors(failures, []error{err})
			continue
		}
		uploaded++
	}
	for _, e := range failures {
		log.Warnf("Test attachment upload failed for %s", e)
	}
	log.Infof("Test attachment upload finished in %s: %d uploaded, %d failed",
		time.Since(start).Round(time.Millisecond), uploaded, len(files)-uploaded)
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
		return err
	}
	realWorkspace, err := filepath.EvalSymlinks(workspace)
	if err != nil {
		return fmt.Errorf("workspace %s: %w", workspace, err)
	}
	if realRoot != realWorkspace && !isWithin(realWorkspace, realRoot) {
		return fmt.Errorf("%s is not inside the workspace", root)
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
