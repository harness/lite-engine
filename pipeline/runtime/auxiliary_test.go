// Copyright 2026 Harness Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package runtime

import (
	"errors"
	"testing"

	"github.com/drone/runner-go/pipeline/runtime"
	"github.com/harness/lite-engine/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConvertPollResponse_SuccessWithUploadErrorSetsAuxiliary(t *testing.T) {
	logStats := &api.LogServiceStats{Upload: api.LogServiceOpStats{Count: 1, ErrorCount: 1}}
	tiStats := &api.TIServiceStats{ReportUpload: api.LogServiceOpStats{Count: 1, ErrorCount: 1}}
	poll := &api.PollStepResponse{
		LogServiceStats: logStats,
		TIServiceStats:  tiStats,
		AuxiliaryStatus: api.AuxiliaryFrom(true, logStats, tiStats),
	}

	resp := convertPollResponseWithErrorDetails(poll, nil, nil)
	assert.Equal(t, api.Success, resp.CommandExecutionStatus)
	require.NotNil(t, resp.AuxiliaryStatus)
	assert.True(t, resp.AuxiliaryStatus.UploadFailed)
	assert.Equal(t, []string{"log_service_upload", "ti_report_upload"}, resp.AuxiliaryStatus.Reasons)
	require.NotNil(t, resp.TIServiceStats)
	assert.Equal(t, int64(1), resp.TIServiceStats.ReportUpload.ErrorCount)
}

func TestConvertPollResponse_FailureOmitsAuxiliary(t *testing.T) {
	poll := &api.PollStepResponse{
		Error: "exit status 1",
		AuxiliaryStatus: &api.AuxiliaryStatus{
			UploadFailed: true,
			Reasons:      []string{"log_service_upload"},
		},
	}

	resp := convertPollResponseWithErrorDetails(poll, nil, nil)
	assert.Equal(t, api.Failure, resp.CommandExecutionStatus)
	assert.Nil(t, resp.AuxiliaryStatus)
}

func TestStepSucceeded(t *testing.T) {
	assert.True(t, stepSucceeded(StepStatus{}))
	assert.False(t, stepSucceeded(StepStatus{StepErr: errors.New("boom")}))
	assert.False(t, stepSucceeded(StepStatus{State: &runtime.State{ExitCode: 1}}))
	assert.False(t, stepSucceeded(StepStatus{State: &runtime.State{OOMKilled: true}}))
	assert.True(t, stepSucceeded(StepStatus{State: &runtime.State{ExitCode: 0}}))
}
