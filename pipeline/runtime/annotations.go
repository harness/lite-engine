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
	defaultPriority     = 3
)

// Annotation modes
const (
	modeReplace = "replace"
	modeAppend  = "append"
	modeDelete  = "delete"
)

// Posting configuration
const (
	postAnnotationsTimeout    = 3 * time.Second
	postAnnotationsMaxRetries = 1
)

// isAnnotationsEnabled returns true if CI_ENABLE_HARNESS_ANNOTATIONS is set to a truthy value
// in the process environment.
func isAnnotationsEnabled(envs map[string]string) bool {
	v := envs[annotationsFFEnv]
	return v == "true"
}

// annotationsFileRaw is the on-disk envelope written by producers (e.g., CLI)
// that lite-engine reads to post annotations to Pipeline Service.
// Keep JSON tags aligned with CLI output.
type annotationsFileRaw struct {
	AccountID        string          `json:"accountId,omitempty"`
	OrgID            string          `json:"orgId,omitempty"`
	ProjectID        string          `json:"projectId,omitempty"`
	PipelineID       string          `json:"pipelineId,omitempty"`
	PlanExecutionID  string          `json:"planExecutionId"`
	StageExecutionID string          `json:"stageExecutionId,omitempty"`
	Annotations      []PMSAnnotation `json:"annotations"`
}

// PMSAnnotation is the typed payload unit sent to Pipeline Service.
// Keep JSON tags aligned with PMS contract.
type PMSAnnotation struct {
	ContextID string `json:"contextId"`
	Mode      string `json:"mode,omitempty"`
	Style     string `json:"style,omitempty"`
	Summary   string `json:"summary,omitempty"`
	Priority  int    `json:"priority,omitempty"`
	Timestamp int64  `json:"timestamp,omitempty"`
	StepID    string `json:"stepId,omitempty"`
}

// createAnnotationsRequest is the final request body for PMS annotations endpoint.
type createAnnotationsRequest struct {
	OrgID            string          `json:"orgId,omitempty"`
	ProjectID        string          `json:"projectId,omitempty"`
	PipelineID       string          `json:"pipelineId,omitempty"`
	StageExecutionID string          `json:"stageExecutionId,omitempty"`
	PlanExecutionID  string          `json:"planExecutionId"`
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
// to Pipeline Service. It never fails the step and logs errors on failures.
func (e *StepExecutor) postAnnotationsToPipeline(ctx context.Context, r *api.StartStepRequest) {
	logrus.WithContext(ctx).
		WithFields(logrus.Fields{
			"step_id":   r.ID,
			"step_name": r.Name,
		}).Infoln("[ANNOTATIONS] Starting postAnnotationsToPipeline")

	// Read annotations file (already validated and parsed)
	file := e.readAnnotationsJSON(r.ID)
	if file == nil {
		logrus.WithContext(ctx).
			WithField("step_id", r.ID).
			Warnln("[ANNOTATIONS] No annotations file found or failed to read, skipping post")
		return
	}

	logrus.WithContext(ctx).
		WithFields(logrus.Fields{
			"step_id":               r.ID,
			"raw_annotation_count":  len(file.Annotations),
			"has_plan_execution_id": file.PlanExecutionID != "",
		}).Infoln("[ANNOTATIONS] Successfully read annotations file")

	// Extract planExecutionID (present in file) and ensure there are annotations
	planExecutionID := file.PlanExecutionID
	if len(file.Annotations) == 0 {
		logrus.WithContext(ctx).
			WithField("step_id", r.ID).
			Infoln("[ANNOTATIONS] No annotations in file, skipping post")
		return
	}

	// Read account identifier from file
	accountID := file.AccountID

	logrus.WithContext(ctx).
		WithFields(logrus.Fields{
			"step_id":             r.ID,
			"has_account_id":      accountID != "",
			"has_org_id":          file.OrgID != "",
			"has_project_id":      file.ProjectID != "",
			"has_pipeline_id":     file.PipelineID != "",
			"has_stage_execution": file.StageExecutionID != "",
		}).Debugln("[ANNOTATIONS] File metadata validation")

	// Fold and sanitize annotations into a final slice
	annList := foldAnnotationsToSlice(file, r.ID)
	if len(annList) == 0 {
		logrus.WithContext(ctx).
			WithField("step_id", r.ID).
			Warnln("[ANNOTATIONS] No annotations after folding/sanitization, skipping post")
		return
	}

	logrus.WithContext(ctx).
		WithFields(logrus.Fields{
			"step_id":        r.ID,
			"final_count":    len(annList),
			"original_count": len(file.Annotations),
		}).Infoln("[ANNOTATIONS] Annotations folded and sanitized")

	if accountID == "" {
		logrus.WithContext(ctx).
			WithField("step_id", r.ID).
			Warnln("[ANNOTATIONS] Missing account ID, cannot post annotations")
		return
	}

	// Build typed request payload directly from file values
	payload := createAnnotationsRequest{
		PlanExecutionID:  planExecutionID,
		Annotations:      annList,
		OrgID:            file.OrgID,
		ProjectID:        file.ProjectID,
		PipelineID:       file.PipelineID,
		StageExecutionID: file.StageExecutionID,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		logrus.WithContext(ctx).
			WithError(err).
			WithField("step_id", r.ID).
			Errorln("[ANNOTATIONS] Failed to marshal payload to JSON")
		return
	}

	logrus.WithContext(ctx).
		WithFields(logrus.Fields{
			"step_id":      r.ID,
			"payload_size": len(body),
		}).Debugln("[ANNOTATIONS] Payload marshaled to JSON")

	// Resolve base URL and token
	base, annToken := resolveAnnotationsConfig(r)

	logrus.WithContext(ctx).
		WithFields(logrus.Fields{
			"step_id":      r.ID,
			"has_base_url": base != "",
			"has_token":    annToken != "",
			"base_url":     base,
		}).Infoln("[ANNOTATIONS] Resolved annotations configuration")

	// Append required identifiers as query params
	endpoint := fmt.Sprintf("/api/pipelines/annotations?accountId=%s&planExecutionId=%s",
		url.QueryEscape(accountID), url.QueryEscape(planExecutionID))

	// Resolve annotations token from merged config (request or setup state)
	if annToken == "" {
		return
	}

	// Prepare request
	client := newPipelineClient(base, annToken, postAnnotationsTimeout)
	for attempt := 1; attempt <= postAnnotationsMaxRetries+1; attempt++ {
		_, _, err := client.PostJSON(ctx, endpoint, body)
		if err == nil {
			return
		}
		logrus.WithField("attempt", attempt).WithField("endpoint", endpoint).WithError(err).Warnln("ANNOTATIONS: post failed")
	}
	logrus.WithField("endpoint", endpoint).Warnln("ANNOTATIONS: post failed after retries")
}

// readAnnotationsJSON reads and parses the per-step annotations file from the shared volume.
// Returns nil if the file does not exist, is too large, or contains invalid JSON.
func (e *StepExecutor) readAnnotationsJSON(stepID string) *annotationsFileRaw {
	if stepID == "" {
		logrus.Debugln("[ANNOTATIONS] Empty step ID, cannot read annotations")
		return nil
	}

	path := fmt.Sprintf("%s/%s-annotations.json", pipeline.SharedVolPath, stepID)
	logrus.WithFields(logrus.Fields{
		"step_id":   stepID,
		"file_path": path,
	}).Infoln("[ANNOTATIONS] Attempting to read annotations file")

	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			logrus.WithFields(logrus.Fields{
				"step_id":   stepID,
				"file_path": path,
			}).Debugln("[ANNOTATIONS] Annotations file does not exist (this is OK if step didn't produce annotations)")
		} else {
			logrus.WithFields(logrus.Fields{
				"step_id":   stepID,
				"file_path": path,
			}).WithError(err).Warnln("[ANNOTATIONS] Error checking file status")
		}
		return nil
	}

	// Cap file size roughly to max annotations * max size per annotation + small overhead (~256KB)
	maxSize := int64(maxAnnotationsCount*maxAnnotationBytes + 256*1024)

	logrus.WithFields(logrus.Fields{
		"step_id":   stepID,
		"file_path": path,
		"file_size": info.Size(),
		"max_size":  maxSize,
	}).Debugln("[ANNOTATIONS] File found, checking size")

	if info.Size() <= 0 {
		logrus.WithFields(logrus.Fields{
			"step_id":   stepID,
			"file_path": path,
		}).Warnln("[ANNOTATIONS] File is empty")
		return nil
	}

	if info.Size() > maxSize {
		logrus.WithFields(logrus.Fields{
			"step_id":   stepID,
			"file_path": path,
			"file_size": info.Size(),
			"max_size":  maxSize,
		}).Warnln("[ANNOTATIONS] File too large, skipping")
		return nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		logrus.WithFields(logrus.Fields{
			"step_id":   stepID,
			"file_path": path,
		}).WithError(err).Errorln("[ANNOTATIONS] Failed to read file")
		return nil
	}

	logrus.WithFields(logrus.Fields{
		"step_id":    stepID,
		"bytes_read": len(data),
		"first_100": func() string {
			if len(data) > 100 {
				return string(data[:100]) + "..."
			}
			return string(data)
		}(),
	}).Debugln("[ANNOTATIONS] File read successfully")

	if !json.Valid(data) {
		logrus.WithFields(logrus.Fields{
			"step_id":   stepID,
			"file_path": path,
			"data_size": len(data),
		}).Errorln("[ANNOTATIONS] Invalid JSON in file")
		return nil
	}

	var file annotationsFileRaw
	if err := json.Unmarshal(data, &file); err != nil {
		logrus.WithFields(logrus.Fields{
			"step_id":   stepID,
			"file_path": path,
		}).WithError(err).Errorln("[ANNOTATIONS] Failed to unmarshal JSON")
		return nil
	}

	originalCount := len(file.Annotations)
	if len(file.Annotations) > maxAnnotationsCount {
		logrus.WithFields(logrus.Fields{
			"step_id":        stepID,
			"original_count": originalCount,
			"max_count":      maxAnnotationsCount,
		}).Warnln("[ANNOTATIONS] Too many annotations, truncating to max")
		file.Annotations = file.Annotations[:maxAnnotationsCount]
	}

	logrus.WithFields(logrus.Fields{
		"step_id":          stepID,
		"annotation_count": len(file.Annotations),
		"truncated":        originalCount > maxAnnotationsCount,
	}).Infoln("[ANNOTATIONS] Successfully parsed annotations file")

	return &file
}

// foldAnnotationsToSlice folds file annotations by context and applies mode semantics.
// It also performs minimal sanitization with last-writer-wins rules.
//
//nolint:gocyclo // The logic handles mode semantics, merging, and clamping in one pass for clarity.
func foldAnnotationsToSlice(file *annotationsFileRaw, id string) []PMSAnnotation {
	annotations := make(map[string]PMSAnnotation)
	for _, a := range file.Annotations {
		ctxName := a.ContextID
		if ctxName == "" {
			continue
		}
		mode := strings.ToLower(a.Mode)
		if mode == "" {
			mode = modeReplace
		}
		switch mode {
		case modeAppend, modeReplace, modeDelete:
		default:
			mode = modeReplace
		}

		if mode == modeDelete {
			entry := PMSAnnotation{ContextID: ctxName, Mode: modeDelete}
			if id != "" {
				entry.StepID = id
			}
			annotations[ctxName] = entry
			continue
		}

		// ensure entry exists
		entry, ok := annotations[ctxName]
		if !ok {
			entry = PMSAnnotation{ContextID: ctxName, Mode: mode}
		}

		// update mode (last writer wins)
		entry.Mode = mode

		// style and priority: last-writer-wins if provided
		if s := strings.ToLower(a.Style); s != "" {
			switch s {
			case "info", "success", "warning", "error":
				entry.Style = s
			default:
				entry.Style = "info"
			}
		}
		if a.Priority < 0 || a.Priority > 10 {
			entry.Priority = defaultPriority
		} else {
			entry.Priority = a.Priority
		}

		// timestamp (optional): last-writer-wins (copy millis)
		if a.Timestamp > 0 {
			entry.Timestamp = a.Timestamp
		}

		// stepId copied from file
		entry.StepID = a.StepID

		// summary (clamped to 64KB). For append mode, clamp the final combined content as well.
		if sum := a.Summary; sum != "" {
			if len(sum) > maxAnnotationBytes {
				sum = sum[:maxAnnotationBytes]
			}
			if entry.Summary != "" && mode == modeAppend {
				combined := entry.Summary + "\n" + sum
				if len(combined) > maxAnnotationBytes {
					combined = combined[:maxAnnotationBytes]
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
func resolveAnnotationsConfig(r *api.StartStepRequest) (base, token string) {
	cfg := r.AnnotationsConfig

	base = strings.TrimSpace(cfg.BaseURL)
	base = strings.TrimRight(base, "/")
	token = strings.TrimSpace(cfg.Token)
	return base, token
}

func newPipelineClient(baseURL, token string, timeout time.Duration) *pipelineClient {
	return &pipelineClient{
		baseURL:    strings.TrimRight(baseURL, "/"),
		token:      token,
		httpClient: &http.Client{Timeout: timeout},
	}
}

// PostJSON posts a JSON body to the given path relative to baseURL.
func (c *pipelineClient) PostJSON(ctx context.Context, path string, body []byte) (status int, respBody []byte, err error) {
	url := c.baseURL + path

	logrus.WithContext(ctx).
		WithFields(logrus.Fields{
			"url":       url,
			"body_size": len(body),
			"has_token": c.token != "",
			"base_url":  c.baseURL,
			"path":      path,
		}).Infoln("[ANNOTATIONS] Preparing HTTP POST request")

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		logrus.WithContext(ctx).
			WithError(err).
			WithField("url", url).
			Errorln("[ANNOTATIONS] Failed to create HTTP request")
		return 0, nil, err
	}

	if c.token != "" {
		// Do not log tokens; set headers only
		req.Header.Set("Authorization", "Annotations "+c.token)
		logrus.WithContext(ctx).Debugln("[ANNOTATIONS] Authorization header set")
	} else {
		logrus.WithContext(ctx).Warnln("[ANNOTATIONS] No token provided for authorization")
	}

	req.Header.Set("Content-Type", "application/json")

	logrus.WithContext(ctx).
		WithFields(logrus.Fields{
			"url":    url,
			"method": "POST",
		}).Infoln("[ANNOTATIONS] Sending HTTP request to pipeline service")

	startTime := time.Now()
	resp, err := c.httpClient.Do(req)
	duration := time.Since(startTime)

	if err != nil {
		logrus.WithContext(ctx).
			WithError(err).
			WithFields(logrus.Fields{
				"url":      url,
				"duration": duration,
			}).Errorln("[ANNOTATIONS] HTTP request failed")
		return 0, nil, err
	}
	defer resp.Body.Close()

	b, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		logrus.WithContext(ctx).
			WithError(readErr).
			WithFields(logrus.Fields{
				"url":         url,
				"status_code": resp.StatusCode,
			}).Warnln("[ANNOTATIONS] Failed to read response body")
	}

	logrus.WithContext(ctx).
		WithFields(logrus.Fields{
			"url":           url,
			"status_code":   resp.StatusCode,
			"duration_ms":   duration.Milliseconds(),
			"response_size": len(b),
			"response_preview": func() string {
				if len(b) > 200 {
					return string(b[:200]) + "..."
				}
				return string(b)
			}(),
		}).Infoln("[ANNOTATIONS] HTTP response received")

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		logrus.WithContext(ctx).
			WithFields(logrus.Fields{
				"url":           url,
				"status_code":   resp.StatusCode,
				"response_body": string(b),
			}).Errorln("[ANNOTATIONS] HTTP request returned non-2xx status code")
	} else {
		logrus.WithContext(ctx).
			WithFields(logrus.Fields{
				"status_code": resp.StatusCode,
				"duration_ms": duration.Milliseconds(),
			}).Infoln("[ANNOTATIONS] Successfully posted annotations to pipeline service")
	}

	return resp.StatusCode, b, nil
}
