// Copyright 2026 Harness Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

// Command ti-cgupload is a temporary test helper that builds the enhanced TI
// callgraph payload exactly like lite-engine and POSTs it to /v2/uploadcg as
// uncompressed JSON. ti-service without a blob store cannot decode the gzip
// body ti-client always sends, so this stands in for the engine upload there.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/harness/lite-engine/internal/filesystem"
	"github.com/harness/lite-engine/ti/callgraph"
	tiCfg "github.com/harness/lite-engine/ti/config"
	"github.com/harness/lite-engine/ti/instrumentation"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "ti-cgupload:", err)
		os.Exit(1)
	}
}

func env(names ...string) string {
	for _, n := range names {
		if v := os.Getenv(n); v != "" {
			return v
		}
	}
	return ""
}

func run() error {
	log := logrus.New()
	ctx := context.Background()

	cgDir := env("TI_OUTPUT_PATH")
	workspace := env("HARNESS_WORKSPACE", "DRONE_WORKSPACE")
	if workspace == "" {
		workspace, _ = os.Getwd()
	}
	endpoint := strings.TrimSuffix(env("HARNESS_TI_SERVICE_ENDPOINT"), "/")
	token := env("HARNESS_TI_SERVICE_TOKEN")
	accountID := env("HARNESS_ACCOUNT_ID")
	orgID := env("HARNESS_ORG_ID")
	projectID := env("HARNESS_PROJECT_ID")
	pipelineID := env("HARNESS_PIPELINE_ID")
	buildID := env("HARNESS_BUILD_ID")
	stageID := env("HARNESS_STAGE_ID")
	stepID := env("HARNESS_STEP_ID")
	repo := env("DRONE_REMOTE_URL")
	sha := env("DRONE_COMMIT_SHA")
	source := env("DRONE_SOURCE_BRANCH", "DRONE_COMMIT_BRANCH")
	target := env("DRONE_TARGET_BRANCH", "DRONE_COMMIT_BRANCH")
	parentUniqueID := env("HARNESS_PARENT_UNIQUE_ID")
	// timeMs is the step duration; ti-service stores it in an int4 column.
	var timeMs int64
	if start, err := strconv.ParseInt(env("TI_STEP_START_MS"), 10, 64); err == nil && start > 0 {
		timeMs = time.Now().UnixMilli() - start
	}

	required := map[string]string{
		"TI_OUTPUT_PATH": cgDir, "HARNESS_TI_SERVICE_ENDPOINT": endpoint, "HARNESS_TI_SERVICE_TOKEN": token,
		"HARNESS_ACCOUNT_ID": accountID, "HARNESS_ORG_ID": orgID, "HARNESS_PROJECT_ID": projectID,
		"HARNESS_PIPELINE_ID": pipelineID, "HARNESS_BUILD_ID": buildID, "HARNESS_STAGE_ID": stageID,
		"HARNESS_STEP_ID": stepID, "DRONE_REMOTE_URL": repo, "DRONE_COMMIT_SHA": sha,
	}
	var missing []string
	for k, v := range required {
		if v == "" {
			missing = append(missing, k)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing env: %s", strings.Join(missing, ", "))
	}

	cgFiles, err := filepath.Glob(filepath.Join(cgDir, "*.json"))
	if err != nil || len(cgFiles) == 0 {
		return fmt.Errorf("no callgraph files in %s (err=%v)", cgDir, err)
	}
	cg, err := callgraph.NewCallGraphParser(log, filesystem.New()).Parse(cgFiles, nil)
	if err != nil {
		return fmt.Errorf("parse callgraph: %w", err)
	}
	fileChecksums, nonCodeConfig, err := instrumentation.GetGitFileChecksums(ctx, workspace, log)
	if err != nil {
		return fmt.Errorf("git file checksums: %w", err)
	}

	cfg := tiCfg.New(endpoint, token, accountID, orgID, projectID, pipelineID, buildID, stageID, repo, sha, "",
		source, target, source, "", parentUniqueID, false, "", "")
	failedFile := env("TI_FAILED_TESTS_FILE_PATH")
	payload, err := callgraph.CreateUploadPayload(cg, fileChecksums, nonCodeConfig, repo, &cfg, sha, nil, log,
		map[string]string{"TI_FAILED_TESTS_FILE_PATH": failedFile})
	if err != nil {
		return fmt.Errorf("create payload: %w", err)
	}
	// Without report tests every path weighs 1 in ti-service's selected count, so the
	// total must also count test files: those that ran plus those TI skipped.
	testFiles := map[string]bool{}
	for _, t := range payload.Tests {
		testFiles[t.Path] = true
	}
	failed := readTestList(failedFile)
	for p := range readTestList(env("TI_SKIP_FILE_PATH")) {
		if !failed[p] {
			testFiles[p] = true
		}
	}
	delete(testFiles, instrumentation.NonCodeChainPath)
	payload.TotalTests = len(testFiles)

	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	q := url.Values{}
	for k, v := range map[string]string{
		"accountId": accountID, "orgId": orgID, "projectId": projectID, "repo": repo,
		"pipelineId": pipelineID, "buildId": buildID, "stageId": stageID, "stepId": stepID,
		"timeMs": fmt.Sprint(timeMs), "sourceBranch": source, "targetBranch": target,
		"parentUniqueId": parentUniqueID,
	} {
		q.Set(k, v)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint+"/v2/uploadcg?"+q.Encode(), bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if strings.HasPrefix(strings.ToLower(token), "pat.") {
		req.Header.Set("x-api-key", token)
	} else {
		req.Header.Set("X-Harness-Token", token)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	resBody, _ := io.ReadAll(res.Body)
	fmt.Printf("ti-cgupload: %d tests, %d chains, totalTests=%d, %d bytes -> HTTP %d %s\n",
		len(payload.Tests), len(payload.Chains), payload.TotalTests, len(body), res.StatusCode, strings.TrimSpace(string(resBody)))
	if res.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("upload failed with HTTP %d", res.StatusCode)
	}
	return nil
}

// readTestList parses a lite-engine skip or failed-tests file the same way the JS
// agent does: whitespace-separated paths, ignoring "file,method" partial entries.
func readTestList(path string) map[string]bool {
	tests := map[string]bool{}
	data, err := os.ReadFile(path)
	if path == "" || err != nil {
		return tests
	}
	for _, entry := range strings.Fields(string(data)) {
		if !strings.Contains(entry, ",") {
			tests[strings.ReplaceAll(entry, `\`, "/")] = true
		}
	}
	return tests
}
