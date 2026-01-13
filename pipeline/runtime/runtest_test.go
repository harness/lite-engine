package runtime

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/harness/lite-engine/api"
	"github.com/harness/lite-engine/ti/callgraph"
	tiCfg "github.com/harness/lite-engine/ti/config"
	"github.com/harness/ti-client/types"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

func Test_CollectRunTestData(t *testing.T) {
	ctx := context.Background()
	log := logrus.New()

	apiReq := api.StartStepRequest{}
	stepName := "RunTests"
	tiConfig := tiCfg.New("app.harness.io", "", "", "", "", "",
		"", "", "", "", "", "", "", "",
		"", "", false, false, "", "")

	telemetryData := types.TelemetryData{}

	tests := []struct {
		name          string
		cgErr         error
		crErr         error
		collectionErr error
	}{
		{
			name:          "NoError",
			cgErr:         nil,
			crErr:         nil,
			collectionErr: nil,
		},
		{
			name:          "CallgraphUploadError",
			cgErr:         fmt.Errorf("callgraph upload error"),
			crErr:         nil,
			collectionErr: fmt.Errorf("failed to collect callgraph: callgraph upload error"),
		},
		{
			name:          "TestReportsUploadError",
			cgErr:         nil,
			crErr:         fmt.Errorf("test reports upload error"),
			collectionErr: nil,
		},
	}

	oldCollectCgFn := collectCgFn
	defer func() { collectCgFn = oldCollectCgFn }()
	oldCollectTestReportsFn := collectTestReportsFn
	defer func() { collectTestReportsFn = oldCollectTestReportsFn }()

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			collectCgFn = func(
				_ context.Context, _ string, _ int64, _ *logrus.Logger, _ time.Time,
				_ *tiCfg.Cfg, _, _ string, _ []*types.TestCase, _ bool,
				_ *api.StartStepRequest,
			) (*callgraph.Callgraph, error) {
				if tc.cgErr != nil {
					return nil, tc.cgErr
				}
				return nil, nil
			}
			collectTestReportsFn = func(
				ctx context.Context,
				report api.TestReport,
				workDir, stepID string,
				log *logrus.Logger,
				start time.Time,
				tiConfig *tiCfg.Cfg,
				testMetadata *types.TestIntelligenceMetaData,
				envs map[string]string,
			) ([]*types.TestCase, error) {
				return []*types.TestCase{}, tc.crErr
			}
			_, err := collectRunTestData(ctx, log, &apiReq, time.Now(), stepName, &tiConfig, &telemetryData)
			assert.Equal(t, tc.collectionErr, err)
		})
	}
}
