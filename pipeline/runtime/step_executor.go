// Copyright 2022 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/harness/lite-engine/api"
	"github.com/harness/lite-engine/engine"
	"github.com/harness/lite-engine/engine/spec"
	"github.com/harness/lite-engine/errors"
	"github.com/harness/lite-engine/livelog"
	"github.com/harness/lite-engine/logstream"
	"github.com/harness/lite-engine/pipeline"
	tiCfg "github.com/harness/lite-engine/ti/config"
	"github.com/harness/lite-engine/ti/report"
	"github.com/harness/ti-client/types"

	"github.com/drone/runner-go/pipeline/runtime"
	"github.com/wings-software/dlite/client"
	"github.com/wings-software/dlite/delegate"

	"github.com/hashicorp/go-multierror"
	"github.com/sirupsen/logrus"
)

type ExecutionStatus int

type RunFunc func(ctx context.Context, step *spec.Step, output io.Writer, isDrone bool, isHosted bool) (*runtime.State, error)

type StepStatus struct {
	Status            ExecutionStatus
	State             *runtime.State
	StepErr           error
	Outputs           map[string]string
	Envs              map[string]string
	Artifact          []byte
	OutputV2          []*api.OutputV2
	OptimizationState string
	TelemetryData     *types.TelemetryData
	Annotations       json.RawMessage
}

// postAnnotationsToPipeline reads the per-step annotations file and posts annotations directly
// to Pipeline Service. It never fails the step and logs warnings on errors.
func (e *StepExecutor) postAnnotationsToPipeline(ctx context.Context, r *api.StartStepRequest) {
	// Gather required identifiers
	accountId := ""
	if r.StepStatus.AccountID != "" {
		accountId = r.StepStatus.AccountID
	}
	planExecutionId := r.Envs["HARNESS_EXECUTION_ID"]

	if accountId == "" || planExecutionId == "" {
		logrus.WithField("id", r.ID).Warnln("annotations: missing accountId or planExecutionId; skipping post")
		return
	}

	raw := e.readAnnotationsJSON(r.ID)
	if raw == nil {
		// nothing to post
		return
	}

	// Parse file envelope
	type fileAnn struct {
		ContextName string `json:"context_name"`
		Timestamp   string `json:"timestamp"`
		Style       string `json:"style"`
		Summary     string `json:"summary"`
		SummaryFile string `json:"summary_file"`
		Priority    int    `json:"priority"`
		Mode        string `json:"mode"`
	}
	var env struct {
		Annotations []fileAnn `json:"annotations"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		logrus.WithField("id", r.ID).WithError(err).Warnln("annotations: invalid JSON; skipping post")
		return
	}
	if len(env.Annotations) == 0 {
		return
	}

	// Fold into map[context]annotationData according to mode semantics (default: replace)
	annotations := make(map[string]map[string]interface{})
	for _, a := range env.Annotations {
		ctxName := strings.TrimSpace(a.ContextName)
		if ctxName == "" {
			continue
		}
		mode := strings.ToLower(strings.TrimSpace(a.Mode))
		if mode == "" {
			mode = "replace"
		}
		if mode != "append" && mode != "replace" && mode != "delete" {
			mode = "replace"
		}

		if mode == "delete" {
			annotations[ctxName] = map[string]interface{}{"mode": "delete"}
			continue
		}

		// ensure entry exists
		if _, ok := annotations[ctxName]; !ok {
			annotations[ctxName] = map[string]interface{}{"summary": "", "mode": mode}
		}
		entry := annotations[ctxName]

		// update mode (last writer wins)
		entry["mode"] = mode

		// style and priority: last-writer-wins if provided
		if strings.TrimSpace(a.Style) != "" {
			entry["style"] = a.Style
		}
		if a.Priority > 0 {
			entry["priority"] = a.Priority
		}

		// summary
		if a.Summary != "" {
			if s, ok := entry["summary"].(string); ok && s != "" && mode == "append" {
				entry["summary"] = s + "\n" + a.Summary
			} else {
				entry["summary"] = a.Summary
			}
		}

		annotations[ctxName] = entry
	}

	if len(annotations) == 0 {
		return
	}

	// Build request payload
	payload := map[string]interface{}{
		"accountId":       accountId,
		"planExecutionId": planExecutionId,
		"annotations":     annotations,
		// Additional metadata for downstream consumers (ignored by server contract)
		"created_at": time.Now().UTC().Format(time.RFC3339),
		"step_id":    r.Name,
		// Aliases for convenience
		"account": accountId,
	}
	if v := strings.TrimSpace(r.Envs["HARNESS_ORG_ID"]); v != "" {
		payload["orgId"] = v
		payload["org"] = v
	}
	if v := strings.TrimSpace(r.Envs["HARNESS_PROJECT_ID"]); v != "" {
		payload["projectId"] = v
		payload["project"] = v
	}
	if v := strings.TrimSpace(r.Envs["HARNESS_PIPELINE_ID"]); v != "" {
		payload["pipelineId"] = v
		payload["pipeline"] = v
	}
	if v := strings.TrimSpace(r.Envs["HARNESS_STAGE_UUID"]); v != "" {
		payload["stageExecutionId"] = v
	} else if v := strings.TrimSpace(r.Envs["HARNESS_STAGE_ID"]); v != "" {
		payload["stageExecutionId"] = v
	}

	body, err := json.Marshal(payload)
	if err != nil {
		logrus.WithField("id", r.ID).WithError(err).Warnln("annotations: failed to marshal request; skipping post")
		return
	}

	// Build URL
	base := strings.TrimRight(strings.TrimSpace(r.StepStatus.Endpoint), "/")
	endpoint := strings.TrimSpace(r.Envs["CI_ANNOTATIONS_ENDPOINT"])
	if endpoint == "" {
		endpoint = "/pipeline/api/pipelines/annotations"
	}

	// Timeout
	timeout := 3 * time.Second
	if ms := strings.TrimSpace(r.Envs["CI_ANNOTATIONS_TIMEOUT_MS"]); ms != "" {
		if v, err := strconv.Atoi(ms); err == nil && v > 0 {
			timeout = time.Duration(v) * time.Millisecond
		}
	}

	// Prepare request
	client := newPipelineClient(base, r.StepStatus.Token, timeout)
	statusCode, err := client.PostJSON(ctx, endpoint, body)
	if err != nil {
		logrus.WithField("id", r.ID).WithError(err).Warnln("annotations: post failed")
		return
	}
	if statusCode < 200 || statusCode >= 300 {
		logrus.WithField("id", r.ID).WithField("status", statusCode).Warnln("annotations: post non-success status")
		return
	}
	logrus.WithField("id", r.ID).Infoln("annotations: post success")
}

// pipelineClient is a minimal HTTP client wrapper for posting annotations
type pipelineClient struct {
	baseURL    string
	token      string
	httpClient *http.Client
}

func newPipelineClient(baseURL, token string, timeout time.Duration) *pipelineClient {
	return &pipelineClient{
		baseURL:    strings.TrimRight(baseURL, "/"),
		token:      token,
		httpClient: &http.Client{Timeout: timeout},
	}
}

func (c *pipelineClient) PostJSON(ctx context.Context, path string, body []byte) (int, error) {
	url := c.baseURL + path
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
		req.Header.Set("X-Harness-Token", c.token)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	return resp.StatusCode, nil
}

const (
	NotStarted ExecutionStatus = iota
	Running
	Complete
	defaultStepTimeout = 10 * time.Hour // default step timeout
	stepStatusUpdate   = "DLITE_CI_VM_EXECUTE_TASK_V2"
	maxStepTimeout     = 24 * 7 * time.Hour // 1 week max timeout
)

type StepExecutor struct {
	engine     *engine.Engine
	mu         sync.Mutex
	stepStatus map[string]StepStatus
	stepLog    map[string]*StepLog
	stepWaitCh map[string][]chan StepStatus
}

func NewStepExecutor(engine *engine.Engine) *StepExecutor {
	return &StepExecutor{
		engine:     engine,
		mu:         sync.Mutex{},
		stepWaitCh: make(map[string][]chan StepStatus),
		stepLog:    make(map[string]*StepLog),
		stepStatus: make(map[string]StepStatus),
	}
}

func (e *StepExecutor) StartStep(ctx context.Context, r *api.StartStepRequest) error {
	if r.ID == "" {
		return &errors.BadRequestError{Msg: "ID needs to be set"}
	}

	e.mu.Lock()
	_, ok := e.stepStatus[r.ID]
	if ok {
		e.mu.Unlock()
		return nil
	}

	e.stepStatus[r.ID] = StepStatus{Status: Running}
	e.mu.Unlock()

	go func() {
		wr := getLogStreamWriter(r)
		state, outputs, envs, artifact, outputV2, telemetrydata, optimizationState, stepErr := e.executeStep(r, wr)
		status := StepStatus{Status: Complete, State: state, StepErr: stepErr, Outputs: outputs, Envs: envs,
			Artifact: artifact, OutputV2: outputV2, OptimizationState: optimizationState, TelemetryData: telemetrydata}
		e.mu.Lock()
		e.stepStatus[r.ID] = status
		channels := e.stepWaitCh[r.ID]
		e.mu.Unlock()

		for _, ch := range channels {
			ch <- status
		}
	}()
	return nil
}

func (e *StepExecutor) StartStepWithStatusUpdate(ctx context.Context, r *api.StartStepRequest) error {
	if r.ID == "" {
		return &errors.BadRequestError{Msg: "ID needs to be set"}
	}

	go func() {
		done := make(chan api.VMTaskExecutionResponse, 1)
		var resp api.VMTaskExecutionResponse
		var wr logstream.Writer

		timeout := time.Duration(r.Timeout) * time.Second
		if timeout < defaultStepTimeout {
			timeout = defaultStepTimeout
		} else if timeout > maxStepTimeout {
			timeout = maxStepTimeout
		}

		go func() {
			if r.StageRuntimeID != "" && r.Image == "" {
				setPrevStepExportEnvs(r)
			}
			wr = getLogStreamWriter(r)
			state, outputs, envs, artifact, outputV2, telemetryData, optimizationState, stepErr := e.executeStep(r, wr)
			status := StepStatus{Status: Complete, State: state, StepErr: stepErr, Outputs: outputs, Envs: envs,
				Artifact: artifact, OutputV2: outputV2, OptimizationState: optimizationState, TelemetryData: telemetryData}
			pollResponse := convertStatus(status)
			// Post annotations directly to Pipeline Service (non-blocking) on success
			if pollResponse.Error == "" && pollResponse.ExitCode == 0 {
				go e.postAnnotationsToPipeline(context.Background(), r)
			}
			if r.StageRuntimeID != "" && len(pollResponse.Envs) > 0 {
				pipeline.GetEnvState().Add(r.StageRuntimeID, pollResponse.Envs)
			}
			resp = convertPollResponse(pollResponse, r.Envs)
			done <- resp
		}()

		select {
		case resp = <-done:
			e.sendStepStatus(r, &resp)
			return
		case <-time.After(timeout):
			// close the log stream if timeout (restore original order)
			if wr != nil {
				wr.Close()
			}
			resp = api.VMTaskExecutionResponse{CommandExecutionStatus: api.Timeout, ErrorMessage: "step timed out"}
			// [ANN_LE] timeout path; skipping annotations POST
			logrus.WithField("tag", "ANN_LE").WithField("id", r.ID).Infoln("[ANN_LE] timeout path; skipping annotations post")
			e.sendStepStatus(r, &resp)
			return
		}
	}()
	return nil
}

func (e *StepExecutor) PollStep(ctx context.Context, r *api.PollStepRequest) (*api.PollStepResponse, error) {
	id := r.ID
	if r.ID == "" {
		return &api.PollStepResponse{}, &errors.BadRequestError{Msg: "ID needs to be set"}
	}

	e.mu.Lock()
	s, ok := e.stepStatus[id]
	if !ok {
		e.mu.Unlock()
		return &api.PollStepResponse{}, &errors.BadRequestError{Msg: "Step has not started"}
	}

	if s.Status == Complete {
		e.mu.Unlock()
		resp := convertStatus(s)
		return resp, nil
	}

	ch := make(chan StepStatus, 1)
	if _, ok := e.stepWaitCh[id]; !ok {
		e.stepWaitCh[id] = append(e.stepWaitCh[id], ch)
	} else {
		e.stepWaitCh[id] = []chan StepStatus{ch}
	}
	e.mu.Unlock()

	status := <-ch
	resp := convertStatus(status)
	return resp, nil
}

func (e *StepExecutor) StreamOutput(ctx context.Context, r *api.StreamOutputRequest) (oldOut []byte, newOut <-chan []byte, err error) {
	id := r.ID
	if id == "" {
		err = &errors.BadRequestError{Msg: "ID needs to be set"}
		return
	}

	var stepLog *StepLog

	// the runner will call this function just before the call to start step, so we wait a while for the step to start
	for ts := time.Now(); ; {
		e.mu.Lock()
		stepLog = e.stepLog[id]
		e.mu.Unlock()

		if stepLog != nil {
			break
		}

		const timeoutDelay = 5 * time.Second
		if time.Since(ts) >= timeoutDelay {
			err = &errors.BadRequestError{Msg: "Step has not started"}
			return
		}

		const retryDelay = 100 * time.Millisecond
		select {
		case <-time.After(retryDelay):
		case <-ctx.Done():
			err = ctx.Err()
			return
		}
	}

	// subscribe to new data messages, and unsubscribe when the request context finished or when the step is done
	chData := make(chan []byte)
	oldOut, err = stepLog.Subscribe(chData, r.Offset)
	if err != nil {
		return
	}

	go func() {
		select {
		case <-ctx.Done():
			// the api request has finished/aborted
		case <-stepLog.Done():
			// the step has finished
		}
		close(chData)
		stepLog.Unsubscribe(chData)
	}()

	newOut = chData

	return //nolint:nakedret
}

func (e *StepExecutor) executeStepDrone(r *api.StartStepRequest) (*runtime.State, error) {
	ctx := context.Background()
	var cancel context.CancelFunc
	if r.Timeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, time.Second*time.Duration(r.Timeout))
	} else {
		ctx, cancel = context.WithCancel(ctx)
	}

	stepLog := NewStepLog(ctx) // step output will terminate when the ctx is canceled

	logr := logrus.WithContext(ctx).
		WithField("id", r.ID).
		WithField("step", r.Name)

	e.mu.Lock()
	e.stepLog[r.ID] = stepLog
	e.mu.Unlock()

	runStep := func() (*runtime.State, error) {
		defer cancel()

		r.Kind = api.Run // only this kind is supported

		exited, _, _, _, _, _, _, err := run(ctx, e.engine.Run, r, stepLog, pipeline.GetState().GetTIConfig())
		if ctx.Err() == context.Canceled || ctx.Err() == context.DeadlineExceeded {
			logr.WithError(err).Warnln("step execution canceled")
			return nil, ctx.Err()
		}
		if err != nil {
			logr.WithError(err).Warnln("step execution failed")
			return nil, err
		}

		if exited != nil {
			if exited.OOMKilled {
				logr.Infoln("step received oom kill")
			} else {
				logr.WithField("exitCode", exited.ExitCode).Infoln("step terminated")
			}
		}

		return exited, nil
	}

	// if the step is configured as a daemon, it is detached
	// from the main process and executed separately.
	if r.Detach {
		go runStep() //nolint:errcheck
		return &runtime.State{Exited: false}, nil
	}

	return runStep()
}

func (e *StepExecutor) executeStep(r *api.StartStepRequest, wr logstream.Writer) (*runtime.State, map[string]string, //nolint:gocritic
	map[string]string, []byte, []*api.OutputV2, *types.TelemetryData, string, error) {
	if r.LogDrone {
		state, err := e.executeStepDrone(r)
		return state, nil, nil, nil, nil, nil, "", err
	}
	// First try to get TI Config from pipeline state, if empty then use the one from step request
	var tiConfig *tiCfg.Cfg
	state := pipeline.GetState()
	if state != nil {
		tiConfig = state.GetTIConfig()
	}
	if (tiConfig == nil || tiConfig.GetURL() == "") && r.TIConfig.URL != "" {
		g := getTiCfg(&r.TIConfig, &r.MtlsConfig)
		tiConfig = &g
	}
	ctx := context.Background()
	return executeStepHelper(ctx, r, e.engine.Run, wr, tiConfig)
}

// executeStepHelper is a helper function which is used both by this step executor as well as the
// stateless step executor. This is done so as to not duplicate logic across multiple implementations.
// Eventually, we should deprecate this step executor in favor of the stateless executor.
func executeStepHelper( //nolint:gocritic
	ctx context.Context,
	r *api.StartStepRequest,
	f RunFunc,
	wr logstream.Writer,
	tiCfg *tiCfg.Cfg) (*runtime.State, map[string]string,
	map[string]string, []byte, []*api.OutputV2, *types.TelemetryData, string, error) {
	// if the step is configured as a daemon, it is detached
	// from the main process and executed separately.
	// We do here only for non-container step.
	if r.Detach && r.Image == "" {
		go func() {
			var cancel context.CancelFunc
			if r.Timeout > 0 {
				ctx, cancel = context.WithTimeout(ctx, time.Second*time.Duration(r.Timeout))
				defer cancel()
			}
			run(ctx, f, r, wr, tiCfg) //nolint:errcheck
			wr.Close()
		}()
		return &runtime.State{Exited: false}, nil, nil, nil, nil, nil, "", nil
	}

	var result error

	var cancel context.CancelFunc
	if r.Timeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, time.Second*time.Duration(r.Timeout))
		defer cancel()
	}

	exited, outputs, envs, artifact, outputV2, telemetryData, optimizationState, err :=
		run(ctx, f, r, wr, tiCfg)
	if err != nil {
		result = multierror.Append(result, err)
	}

	// if err is not nill or it's not a detach step then always close the stream
	if err != nil || !r.Detach {
		// close the stream. If the session is a remote session, the
		// full log buffer is uploaded to the remote server.
		if err = wr.Close(); err != nil {
			result = multierror.Append(result, err)
		}
	}

	// if the context was canceled and returns a canceled or
	// DeadlineExceeded error this indicates the step was timed out.
	switch ctx.Err() {
	case context.Canceled, context.DeadlineExceeded:
		return nil, nil, nil, nil, nil, nil, "", ctx.Err()
	}

	if exited != nil {
		if exited.ExitCode != 0 {
			if wr.Error() != nil {
				result = multierror.Append(result, err)
			}
		}

		if exited.OOMKilled {
			logrus.WithContext(ctx).WithField("id", r.ID).Infoln("received oom kill.")
		} else {
			logrus.WithContext(ctx).WithField("id", r.ID).Infof("received exit code %d\n", exited.ExitCode)
		}
	}
	return exited, outputs, envs, artifact, outputV2, telemetryData, optimizationState, result
}

func run(ctx context.Context, f RunFunc, r *api.StartStepRequest, out io.Writer, tiConfig *tiCfg.Cfg) ( //nolint:gocritic
	*runtime.State, map[string]string, map[string]string, []byte, []*api.OutputV2, *types.TelemetryData, string, error) {
	if r.Kind == api.Run {
		return executeRunStep(ctx, f, r, out, tiConfig)
	}
	if r.Kind == api.RunTestsV2 {
		return executeRunTestsV2Step(ctx, f, r, out, tiConfig)
	}
	return executeRunTestStep(ctx, f, r, out, tiConfig)
}

func getLogStreamWriter(r *api.StartStepRequest) logstream.Writer {
	if r.LogDrone {
		return nil
	}
	pipelineState := pipeline.GetState()
	secrets := append(pipelineState.GetSecrets(), r.Secrets...)

	// Create a log stream for step logs
	client := pipelineState.GetLogStreamClient()

	wc := livelog.New(client, r.LogKey, r.Name, getNudges(), false, pipelineState.GetLogConfig().TrimNewLineSuffix, pipelineState.GetLogConfig().SkipOpeningStream)
	wr := logstream.NewReplacerWithEnvs(wc, secrets, r.Envs)
	go wr.Open() //nolint:errcheck
	return wr
}

// This is used for Github Actions to set the envs from prev step.
// TODO: This needs to be changed once HARNESS_ENV changes come
func setPrevStepExportEnvs(r *api.StartStepRequest) {
	prevStepExportEnvs := pipeline.GetEnvState().Get(r.StageRuntimeID)
	for k, v := range prevStepExportEnvs {
		if r.Envs == nil {
			r.Envs = make(map[string]string)
		}
		r.Envs[k] = v
	}
}

func (e *StepExecutor) sendStepStatus(r *api.StartStepRequest, response *api.VMTaskExecutionResponse) {
	delegateClient := delegate.NewFromToken(r.StepStatus.Endpoint, r.StepStatus.AccountID, r.StepStatus.Token, true, "")

	if err := e.sendStatus(r, delegateClient, response); err != nil {
		logrus.WithField("id", r.ID).WithError(err).Errorln("failed to send step status")
		return
	}
	logrus.WithField("id", r.ID).Infoln("successfully sent step status")
}

func (e *StepExecutor) sendStatus(r *api.StartStepRequest, delegateClient *delegate.HTTPClient, response *api.VMTaskExecutionResponse) error {
	if r.StepStatus.RunnerResponse {
		return e.sendRunnerResponseStatus(r, delegateClient, response)
	} else if r.StepStatus.TaskStatusV2 {
		return e.sendResponseStatusV2(r, delegateClient, response)
	} else {
		return e.sendResponseStatus(r, delegateClient, response)
	}
}

func (e *StepExecutor) sendRunnerResponseStatus(r *api.StartStepRequest, delegateClient *delegate.HTTPClient, response *api.VMTaskExecutionResponse) error {
	logrus.WithField("id", r.ID).Infoln("Sending runner step status")
	// [ANN_LE] sending runner step status (no annotations attached)
	taskResponse := getRunnerTaskResponse(r, response)
	return delegateClient.SendRunnerStatus(context.Background(), r.StepStatus.DelegateID, r.StepStatus.TaskID, taskResponse)
}

func (e *StepExecutor) sendResponseStatusV2(r *api.StartStepRequest, delegateClient *delegate.HTTPClient, response *api.VMTaskExecutionResponse) error {
	logrus.WithField("id", r.ID).Infoln("Sending step status to V2 Endpoint")
	// [ANN_LE] sending step status (no annotations attached)
	taskResponse := getRunnerTaskResponse(r, response)
	return delegateClient.SendStatusV2(context.Background(), r.StepStatus.DelegateID, r.StepStatus.TaskID, taskResponse)
}

func (e *StepExecutor) sendResponseStatus(r *api.StartStepRequest, delegateClient *delegate.HTTPClient, response *api.VMTaskExecutionResponse) error {
	// For legacy backwards compatibility treat timeout as failure
	if response.CommandExecutionStatus == api.Timeout {
		response.CommandExecutionStatus = api.Failure
	}
	// [ANN_LE] sending legacy step status (no annotations attached)
	jsonData, err := json.Marshal(response)
	if err != nil {
		return err
	}
	taskResponse := &client.TaskResponse{
		Data: json.RawMessage(jsonData),
		Code: "OK",
		Type: stepStatusUpdate,
	}
	return delegateClient.SendStatus(context.Background(), r.StepStatus.DelegateID, r.StepStatus.TaskID, taskResponse)
}

func getRunnerTaskResponse(r *api.StartStepRequest, response *api.VMTaskExecutionResponse) *client.RunnerTaskResponse {
	status := client.Success
	if response.CommandExecutionStatus == api.Failure {
		status = client.Failure
	} else if response.CommandExecutionStatus == api.Timeout {
		status = client.Timeout
	}

	jsonData, err := json.Marshal(response)
	// In case of invalid response data, send failure response
	if err != nil {
		logrus.WithField("id", r.ID).WithError(err).Errorln("failed to marshal the response, failing the task")
		response.ErrorMessage = "Failed to marshal the response data"
		status = client.Failure
	}
	// [ANN_LE] response marshalled (no annotations attached)

	return &client.RunnerTaskResponse{
		ID:    r.StepStatus.TaskID,
		Data:  json.RawMessage(jsonData),
		Code:  status,
		Error: response.ErrorMessage,
		Type:  stepStatusUpdate,
	}
}

// convertStatus converts StepStatus to PollStepResponse
func convertStatus(status StepStatus) *api.PollStepResponse {
	r := &api.PollStepResponse{
		Outputs:           status.Outputs,
		Envs:              status.Envs,
		Artifact:          status.Artifact,
		OutputV2:          status.OutputV2,
		OptimizationState: status.OptimizationState,
		TelemetryData:     status.TelemetryData,
		Annotations:       status.Annotations,
	}
	// If the step has reached Complete, mark Exited=true even if state is nil.
	if status.Status == Complete {
		r.Exited = true
	}
	if status.State != nil {
		// Preserve explicit runtime state; ensure we don't downgrade a completed step to Exited=false.
		r.Exited = r.Exited || status.State.Exited
		r.ExitCode = status.State.ExitCode
		r.OOMKilled = status.State.OOMKilled
	}
	if status.StepErr != nil {
		r.Error = status.StepErr.Error()
	}
	return r
}

func convertPollResponse(r *api.PollStepResponse, envs map[string]string) api.VMTaskExecutionResponse {
	if r.Error == "" {
		return api.VMTaskExecutionResponse{
			CommandExecutionStatus: api.Success,
			OutputVars:             r.Outputs,
			Artifact:               r.Artifact,
			Outputs:                r.OutputV2,
			OptimizationState:      r.OptimizationState,
			TelemetryData:          r.TelemetryData,
		}
	}
	if report.TestSummaryAsOutputEnabled(envs) {
		return api.VMTaskExecutionResponse{
			CommandExecutionStatus: api.Failure,
			OutputVars:             r.Outputs,
			Outputs:                r.OutputV2,
			ErrorMessage:           r.Error,
			OptimizationState:      r.OptimizationState,
			TelemetryData:          r.TelemetryData,
		}
	}
	return api.VMTaskExecutionResponse{
		CommandExecutionStatus: api.Failure,
		ErrorMessage:           r.Error,
		OptimizationState:      r.OptimizationState,
	}
}

func (e *StepExecutor) readAnnotationsJSON(stepID string) json.RawMessage {
	if stepID == "" {
		return nil
	}
	path := fmt.Sprintf("%s/%s-annotations.json", pipeline.SharedVolPath, stepID)
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			logrus.WithField("tag", "ANN_LE").WithField("step_id", stepID).WithField("path", path).Infoln("[ANN_LE] annotations: file not found")
		} else {
			logrus.WithField("step_id", stepID).WithField("path", path).WithError(err).Debugln("annotations: stat failed")
			logrus.WithField("tag", "ANN_LE").WithField("step_id", stepID).WithField("path", path).WithError(err).Infoln("[ANN_LE] annotations: stat failed")
		}
		return nil
	}
	const maxSize = 10 * 1024 * 1024 // 5MB cap
	if info.Size() <= 0 {
		logrus.WithField("tag", "ANN_LE").WithField("step_id", stepID).WithField("path", path).Infoln("[ANN_LE] annotations: file empty")
		return nil
	}
	if info.Size() > maxSize {
		logrus.WithField("step_id", stepID).WithField("path", path).WithField("size", info.Size()).Warnln("annotations: file too large, skipping")
		logrus.WithField("tag", "ANN_LE").WithField("step_id", stepID).WithField("path", path).WithField("size", info.Size()).Infoln("[ANN_LE] annotations: file too large, skipping")
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		logrus.WithField("step_id", stepID).WithField("path", path).WithError(err).Debugln("annotations: read failed")
		logrus.WithField("tag", "ANN_LE").WithField("step_id", stepID).WithField("path", path).WithError(err).Infoln("[ANN_LE] annotations: read failed")
		return nil
	}
	if !json.Valid(data) {
		logrus.WithField("step_id", stepID).WithField("path", path).Warnln("annotations: invalid JSON, skipping")
		logrus.WithField("tag", "ANN_LE").WithField("step_id", stepID).WithField("path", path).Infoln("[ANN_LE] annotations: invalid JSON, skipping")
		return nil
	}
	logrus.WithField("step_id", stepID).WithField("path", path).WithField("bytes", len(data)).Debugln("annotations: attached JSON")
	logrus.WithField("tag", "ANN_LE").WithField("step_id", stepID).WithField("path", path).WithField("bytes", len(data)).Infoln("[ANN_LE] annotations: attached JSON")
	return json.RawMessage(data)
}
