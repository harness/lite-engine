package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/harness/lite-engine/api"
	"github.com/harness/lite-engine/pipeline"
	"github.com/sirupsen/logrus"
)

// Feature flag to enable/disable pipeline annotations end-to-end
const annotationsFFEnv = "CI_ENABLE_HARNESS_ANNOTATIONS"

// Per-file and per-annotation limits
const (
	maxAnnotationBytes  = 64 * 1024 // 64KB per annotation
	maxAnnotationsCount = 50        // max annotations per file
)

// isAnnotationsEnabled returns true if CI_ENABLE_HARNESS_ANNOTATIONS is set to a truthy value
// in the process environment.
func isAnnotationsEnabled(envs map[string]string) bool {
	v := envs[annotationsFFEnv]
	return v == "true"
}

// truncateUTF8ByBytes truncates a string to the provided byte limit without breaking UTF-8 runes.
func truncateUTF8ByBytes(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	// Iterate over rune boundaries; keep the last index <= limit
	last := 0
	for i := range s {
		if i > limit {
			break
		}
		last = i
	}
	if last == 0 {
		return ""
	}
	return s[:last]
}

// annotationsFileRaw is the on-disk envelope written by producers (e.g., CLI)
// that lite-engine reads to post annotations to Pipeline Service.
// Keep JSON tags aligned with CLI output.
type annotationsFileRaw struct {
	PlanExecutionId string                `json:"planExecutionId"`
	Annotations     []annotationFileEntry `json:"annotations"`
}

// annotationFileEntry mirrors a single annotation entry on disk.
// Keep JSON tags aligned with CLI's AnnotationEntry.
type annotationFileEntry struct {
	// PMS-aligned keys
	ContextId string `json:"contextId"`
	StepId    string `json:"stepId,omitempty"`
	Timestamp int64  `json:"timestamp"`
	Style     string `json:"style"`
	Summary   string `json:"summary"`
	Priority  int    `json:"priority"`
	Mode      string `json:"mode,omitempty"`
}

// PMSAnnotation is the typed payload unit sent to Pipeline Service.
// Keep JSON tags aligned with PMS contract.
type PMSAnnotation struct {
	ContextId string `json:"contextId"`
	Mode      string `json:"mode,omitempty"`
	Style     string `json:"style,omitempty"`
	Summary   string `json:"summary,omitempty"`
	Priority  int    `json:"priority,omitempty"`
	Timestamp int64  `json:"timestamp,omitempty"`
	StepId    string `json:"stepId,omitempty"`
}

// createAnnotationsRequest is the final request body for PMS annotations endpoint.
type createAnnotationsRequest struct {
	AccountId        string          `json:"accountId"`
	OrgId            string          `json:"orgId,omitempty"`
	ProjectId        string          `json:"projectId,omitempty"`
	PipelineId       string          `json:"pipelineId,omitempty"`
	StageExecutionId string          `json:"stageExecutionId,omitempty"`
	PlanExecutionId  string          `json:"planExecutionId"`
	Annotations      []PMSAnnotation `json:"annotations"`
}

// pipelineClient is a minimal HTTP client wrapper for posting annotations
// to Pipeline Service with a dedicated token.
type pipelineClient struct {
	baseURL    string
	token      string
	httpClient *http.Client
}

// postAnnotationsToPipeline reads the per-step annotations file and posts annotations directly
// to Pipeline Service. It never fails the step and logs warnings on errors.
func (e *StepExecutor) postAnnotationsToPipeline(ctx context.Context, r *api.StartStepRequest) {
	// Defense-in-depth note:
	// Even though the CLI normalizes/sanitizes annotations when writing the file,
	// other producers (plugins, scripts, manual edits) may create or modify it.
	// We intentionally re-validate and default fields here before POSTing to PMS.

	logrus.WithContext(ctx).WithField("id", r.ID).Infoln("ANNOTATIONS: starting annotations post")

	// Gather account identifier strictly from step env: HARNESS_ACCOUNT_ID
	accountId := strings.TrimSpace(r.Envs["HARNESS_ACCOUNT_ID"])

	// Read annotations file (also carries planExecutionId now)
	raw := e.readAnnotationsJSON(r.ID)
	if raw == nil {
		logrus.WithContext(ctx).WithField("id", r.ID).Infoln("ANNOTATIONS: no annotations file or invalid; skipping post")
		return
	}
	logrus.WithContext(ctx).WithFields(logrus.Fields{"id": r.ID, "bytes": len(raw)}).Infoln("ANNOTATIONS: read annotations file")

	// Parse file envelope (typed) and extract planExecutionId
	var file annotationsFileRaw
	if err := json.Unmarshal(raw, &file); err != nil {
		logrus.WithField("id", r.ID).WithError(err).Warnln("ANNOTATIONS: invalid JSON; skipping post")
		return
	}
	logrus.WithContext(ctx).WithFields(logrus.Fields{"id": r.ID, "count": len(file.Annotations)}).Infoln("ANNOTATIONS: parsed annotations")
	// Enforce maximum annotations per file
	if len(file.Annotations) > maxAnnotationsCount {
		logrus.WithFields(logrus.Fields{"id": r.ID, "count": len(file.Annotations)}).Warnln("ANNOTATIONS: too many annotations; truncating to 50")
		file.Annotations = file.Annotations[:maxAnnotationsCount]
	}
	planExecutionId := strings.TrimSpace(file.PlanExecutionId)
	if planExecutionId == "" || len(file.Annotations) == 0 {
		logrus.WithContext(ctx).WithField("id", r.ID).Infoln("ANNOTATIONS: missing planExecutionId or empty annotations; skipping post")
		return
	}

	// Fold and sanitize annotations into a final slice
	annList := foldAnnotationsToSlice(file, r.ID)
	if len(annList) == 0 {
		logrus.WithContext(ctx).WithField("id", r.ID).Infoln("ANNOTATIONS: folded annotations empty; skipping post")
		return
	}
	logrus.WithContext(ctx).WithFields(logrus.Fields{"id": r.ID, "count": len(annList)}).Infoln("ANNOTATIONS: folded annotations ready")

	// Ensure we have required identifiers
	if accountId == "" {
		logrus.WithField("id", r.ID).Warnln("ANNOTATIONS: missing accountId; skipping post")
		return
	}

	// Build typed request payload
	payload := createAnnotationsRequest{
		AccountId:       accountId,
		PlanExecutionId: planExecutionId,
		Annotations:     annList,
	}
	if v := strings.TrimSpace(r.Envs["HARNESS_ORG_ID"]); v != "" {
		payload.OrgId = v
	}
	if v := strings.TrimSpace(r.Envs["HARNESS_PROJECT_ID"]); v != "" {
		payload.ProjectId = v
	}
	if v := strings.TrimSpace(r.Envs["HARNESS_PIPELINE_ID"]); v != "" {
		payload.PipelineId = v
	}
	if v := strings.TrimSpace(r.Envs["HARNESS_STAGE_UUID"]); v != "" {
		payload.StageExecutionId = v
	} else if v := strings.TrimSpace(r.Envs["HARNESS_STAGE_ID"]); v != "" {
		payload.StageExecutionId = v
	}

	body, err := json.Marshal(payload)
	if err != nil {
		logrus.WithField("id", r.ID).WithError(err).Warnln("ANNOTATIONS: failed to marshal request; skipping post")
		return
	}

	// Resolve base URL and token
	base, annToken := resolveAnnotationsConfig(r)

	// Append required identifiers as query params
	endpoint := fmt.Sprintf("/api/pipelines/annotations?accountId=%s&planExecutionId=%s",
		url.QueryEscape(accountId), url.QueryEscape(planExecutionId))
	fullURL := base + endpoint
	logrus.WithContext(ctx).WithFields(logrus.Fields{"id": r.ID, "base": base, "endpoint": endpoint}).Infoln("ANNOTATIONS: resolved URL components")

	// Timeout
	timeout := 3 * time.Second

	// Resolve annotations token from merged config (request or setup state)
	if annToken == "" {
		logrus.WithField("id", r.ID).Warnln("ANNOTATIONS: missing annotations token; skipping post")
		return
	}

	// Prepare request
	client := newPipelineClient(base, annToken, timeout)
	statusCode, respBody, err := client.PostJSON(ctx, endpoint, body)
	if err != nil {
		logrus.WithFields(logrus.Fields{"id": r.ID, "url": fullURL}).WithError(err).Warnln("ANNOTATIONS: post failed")
		return
	}
	if statusCode < 200 || statusCode >= 300 {
		logrus.WithFields(logrus.Fields{
			"id":     r.ID,
			"status": statusCode,
			"bytes":  len(respBody),
		}).Warnln("ANNOTATIONS: post non-success status")
		return
	}
	logrus.WithFields(logrus.Fields{
		"id":     r.ID,
		"status": statusCode,
	}).Infoln("ANNOTATIONS: post success")
}

// readAnnotationsJSON reads the per-step annotations file from the shared volume.
// Returns nil if the file does not exist, is too large, or contains invalid JSON.
func (e *StepExecutor) readAnnotationsJSON(stepID string) json.RawMessage {
	if stepID == "" {
		return nil
	}
	path := fmt.Sprintf("%s/%s-annotations.json", pipeline.SharedVolPath, stepID)
	info, err := os.Stat(path)
	if err != nil {
		// file may legitimately not exist if no producer wrote annotations
		if !os.IsNotExist(err) {
			logrus.WithField("step_id", stepID).WithField("path", path).WithError(err).Debugln("ANNOTATIONS: stat failed")
		}
		return nil
	}
	// Cap file size roughly to max annotations * max size per annotation + small overhead (~256KB)
	maxSize := int64(maxAnnotationsCount*maxAnnotationBytes + 256*1024)
	if info.Size() <= 0 {
		return nil
	}
	if info.Size() > maxSize {
		logrus.WithField("step_id", stepID).WithField("path", path).WithField("size", info.Size()).Warnln("ANNOTATIONS: file too large, skipping")
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		logrus.WithField("step_id", stepID).WithField("path", path).WithError(err).Debugln("ANNOTATIONS: read failed")
		return nil
	}
	if !json.Valid(data) {
		logrus.WithField("step_id", stepID).WithField("path", path).Warnln("ANNOTATIONS: invalid JSON, skipping")
		return nil
	}
	return json.RawMessage(data)
}

// foldAnnotationsToSlice folds file annotations by context and applies mode semantics.
// It also performs minimal sanitization with last-writer-wins rules.
func foldAnnotationsToSlice(file annotationsFileRaw, id string) []PMSAnnotation {
	annotations := make(map[string]PMSAnnotation)
	for _, a := range file.Annotations {
		ctxName := strings.TrimSpace(a.ContextId)
		if ctxName == "" {
			continue
		}
		mode := strings.ToLower(strings.TrimSpace(a.Mode))
		if mode == "" {
			mode = "replace"
		}
		switch mode {
		case "append", "replace", "delete":
		default:
			mode = "replace"
		}

		if mode == "delete" {
			entry := PMSAnnotation{ContextId: ctxName, Mode: "delete"}
			if id != "" {
				entry.StepId = id
			}
			annotations[ctxName] = entry
			continue
		}

		// ensure entry exists
		entry, ok := annotations[ctxName]
		if !ok {
			entry = PMSAnnotation{ContextId: ctxName, Mode: mode}
		}

		// update mode (last writer wins)
		entry.Mode = mode

		// style and priority: last-writer-wins if provided
		if s := strings.ToLower(strings.TrimSpace(a.Style)); s != "" {
			switch s {
			case "info", "success", "warning", "error":
				entry.Style = s
			default:
				entry.Style = "info"
			}
		}
		if a.Priority < 0 {
			entry.Priority = 3
		} else {
			entry.Priority = a.Priority
		}

		// timestamp (optional): last-writer-wins (copy millis)
		if a.Timestamp > 0 {
			entry.Timestamp = a.Timestamp
		}

		// stepId: use provided stepId or fallback to runtime step id
		if s := strings.TrimSpace(a.StepId); s != "" {
			entry.StepId = s
		} else if id != "" {
			entry.StepId = id
		}

		// summary (clamped to 64KB). For append mode, clamp the final combined content as well.
		if sum := a.Summary; sum != "" {
			if len(sum) > maxAnnotationBytes {
				sum = truncateUTF8ByBytes(sum, maxAnnotationBytes)
			}
			if entry.Summary != "" && mode == "append" {
				combined := entry.Summary + "\n" + sum
				if len(combined) > maxAnnotationBytes {
					combined = truncateUTF8ByBytes(combined, maxAnnotationBytes)
				}
				entry.Summary = combined
			} else {
				entry.Summary = sum
			}
		}

		annotations[ctxName] = entry
	}

	if len(annotations) == 0 {
		return nil
	}

	// Convert map to array for API contract
	annList := make([]PMSAnnotation, 0, len(annotations))
	for _, v := range annotations {
		annList = append(annList, v)
	}
	return annList
}

// resolveAnnotationsConfig merges BaseURL + Token from request, or step endpoint origin.
func resolveAnnotationsConfig(r *api.StartStepRequest) (string, string) {
	cfg := r.AnnotationsConfig

	base := strings.TrimSpace(cfg.BaseURL)
	if base == "" {
		raw := strings.TrimSpace(r.StepStatus.Endpoint)
		if u, err := url.Parse(raw); err == nil && u.Scheme != "" && u.Host != "" {
			base = u.Scheme + "://" + u.Host
		} else {
			base = raw
		}
	}
	base = strings.TrimRight(base, "/")
	return base, strings.TrimSpace(cfg.Token)
}

func newPipelineClient(baseURL, token string, timeout time.Duration) *pipelineClient {
	return &pipelineClient{
		baseURL:    strings.TrimRight(baseURL, "/"),
		token:      token,
		httpClient: &http.Client{Timeout: timeout},
	}
}

// PostJSON posts a JSON body to the given path relative to baseURL.
func (c *pipelineClient) PostJSON(ctx context.Context, path string, body []byte) (int, []byte, error) {
	url := c.baseURL + path
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return 0, nil, err
	}
	if c.token != "" {
		// Do not log tokens; set headers only
		req.Header.Set("Authorization", "Annotations "+c.token)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, b, nil
}
