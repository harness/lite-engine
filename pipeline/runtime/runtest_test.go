package runtime

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/harness/lite-engine/api"
	tiCfg "github.com/harness/lite-engine/ti/config"
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
		"", false)

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
			collectionErr: fmt.Errorf("callgraph upload error"),
		},
		{
			name:          "TestReportsUploadError",
			cgErr:         nil,
			crErr:         fmt.Errorf("test reports upload error"),
			collectionErr: fmt.Errorf("test reports upload error"),
		},
	}

	oldCollectCgFn := collectCgFn
	defer func() { collectCgFn = oldCollectCgFn }()
	oldCollectTestReportsFn := collectTestReportsFn
	defer func() { collectTestReportsFn = oldCollectTestReportsFn }()

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			collectCgFn = func(ctx context.Context, stepID string, timeMs int64, log *logrus.Logger, start time.Time, tiConfig *tiCfg.Cfg) error {
				return tc.cgErr
			}
			collectTestReportsFn = func(ctx context.Context, report api.TestReport, workDir, stepID string, log *logrus.Logger, start time.Time, tiConfig *tiCfg.Cfg) error {
				return tc.crErr
			}
			err := collectRunTestData(ctx, log, &apiReq, time.Now(), stepName, &tiConfig)
			assert.Equal(t, tc.collectionErr, err)
		})
	}
}
