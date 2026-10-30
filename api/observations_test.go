// Copyright 2026 Harness Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package api

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStepObservations_RecordsReportAndCallgraph(t *testing.T) {
	obs := &StepObservations{}
	obs.RecordReportUpload(nil, 20*time.Millisecond)
	obs.RecordReportUpload(errors.New("ti down"), 5*time.Millisecond)
	obs.RecordCallgraphUpload(errors.New("cg down"), time.Millisecond)

	stats := obs.TIStats()
	require.NotNil(t, stats)
	assert.Equal(t, int64(2), stats.ReportUpload.Count)
	assert.Equal(t, int64(1), stats.ReportUpload.ErrorCount)
	assert.Equal(t, int64(20), stats.ReportUpload.LatencyMs)
	assert.Equal(t, int64(1), stats.CallgraphUpload.Count)
	assert.Equal(t, int64(1), stats.CallgraphUpload.ErrorCount)
	assert.Equal(t, int64(0), stats.CallgraphUpload.LatencyMs)
}

func TestStepObservations_EmptyIsNil(t *testing.T) {
	assert.Nil(t, (*StepObservations)(nil).TIStats())
	assert.Nil(t, (&StepObservations{}).TIStats())
}

func TestObservationsRoundTripOnContext(t *testing.T) {
	obs := &StepObservations{}
	ctx := ContextWithObservations(context.Background(), obs)
	require.Same(t, obs, ObservationsFromContext(ctx))
	assert.Nil(t, ObservationsFromContext(context.Background()))
	assert.Nil(t, ContextWithObservations(nil, obs))
}

func TestAuxiliaryFrom(t *testing.T) {
	logFailed := &LogServiceStats{Upload: LogServiceOpStats{Count: 1, ErrorCount: 1}}
	tiFailed := &TIServiceStats{ReportUpload: LogServiceOpStats{Count: 1, ErrorCount: 1}}

	assert.Nil(t, AuxiliaryFrom(false, logFailed, tiFailed))
	assert.Nil(t, AuxiliaryFrom(true, &LogServiceStats{}, &TIServiceStats{}))

	got := AuxiliaryFrom(true, logFailed, tiFailed)
	require.NotNil(t, got)
	assert.True(t, got.UploadFailed)
	assert.Equal(t, []string{"log_service_upload", "ti_report_upload"}, got.Reasons)

	closeFailed := AuxiliaryFrom(true, &LogServiceStats{Close: LogServiceOpStats{Count: 1, ErrorCount: 1}}, nil)
	require.NotNil(t, closeFailed)
	assert.Equal(t, []string{"log_service_close"}, closeFailed.Reasons)
}
