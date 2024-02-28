package runtime

import (
	"context"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/harness/lite-engine/api"
	"github.com/harness/lite-engine/internal/filesystem"
	tiCfg "github.com/harness/lite-engine/ti/config"
	"github.com/harness/ti-client/types"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

func Test_CollectRunTestsV2Data(t *testing.T) {
	ctx := context.Background()
	log := logrus.New()

	apiReq := api.StartStepRequest{}
	stepName := "RunTestsV2"
	tiConfig := tiCfg.New("app.harness.io", "", "", "", "", "",
		"", "", "", "", "", "", "", "",
		"", false, false)

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
			collectCgFn = func(ctx context.Context, stepID string, timeMs int64, log *logrus.Logger, start time.Time, tiConfig *tiCfg.Cfg, dir string) error {
				return tc.cgErr
			}
			collectTestReportsFn = func(ctx context.Context, report api.TestReport, workDir, stepID string, log *logrus.Logger, start time.Time, tiConfig *tiCfg.Cfg, envs map[string]string) error {
				return tc.crErr
			}
			err := collectTestReportsAndCg(ctx, log, &apiReq, time.Now(), stepName, &tiConfig)
			assert.Equal(t, tc.collectionErr, err)
		})
	}
}

func Test_createSelectedTestFile(t *testing.T) {
	type args struct {
		ctx            context.Context
		fs             filesystem.FileSystem
		stepID         string
		workspace      string
		log            *logrus.Logger
		tiConfig       *tiCfg.Cfg
		tmpFilepath    string
		envs           map[string]string
		runV2Config    *api.RunTestsV2Config
		filterFilePath string
	}
	tests := []struct {
		name    string
		args    args
		wantErr bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := createSelectedTestFile(tt.args.ctx, tt.args.fs, tt.args.stepID, tt.args.workspace, tt.args.log, tt.args.tiConfig, tt.args.tmpFilepath, tt.args.envs, tt.args.runV2Config, tt.args.filterFilePath); (err != nil) != tt.wantErr {
				t.Errorf("createSelectedTestFile() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func Test_downloadJavaAgent(t *testing.T) {
	type args struct {
		ctx  context.Context
		path string
		fs   filesystem.FileSystem
		log  *logrus.Logger
	}
	tests := []struct {
		name    string
		args    args
		wantErr bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := downloadJavaAgent(tt.args.ctx, tt.args.path, tt.args.fs, tt.args.log); (err != nil) != tt.wantErr {
				t.Errorf("downloadJavaAgent() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func Test_getPreCmd(t *testing.T) {
	type args struct {
		tmpFilePath string
		fs          filesystem.FileSystem
		log         *logrus.Logger
		envs        map[string]string
		runV2Config *api.RunTestsV2Config
	}
	tests := []struct {
		name    string
		args    args
		want    string
		want1   string
		wantErr bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, got1, err := getPreCmd(tt.args.tmpFilePath, tt.args.fs, tt.args.log, tt.args.envs, tt.args.runV2Config)
			if (err != nil) != tt.wantErr {
				t.Errorf("getPreCmd() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("getPreCmd() got = %v, want %v", got, tt.want)
			}
			if got1 != tt.want1 {
				t.Errorf("getPreCmd() got1 = %v, want %v", got1, tt.want1)
			}
		})
	}
}

func Test_createJavaConfigFile(t *testing.T) {
	type args struct {
		tmpDir   string
		fs       filesystem.FileSystem
		log      *logrus.Logger
		splitIdx int
	}
	tests := []struct {
		name    string
		args    args
		want    string
		want1   string
		wantErr bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, got1, err := createJavaConfigFile(tt.args.tmpDir, tt.args.fs, tt.args.log, tt.args.splitIdx)
			if (err != nil) != tt.wantErr {
				t.Errorf("createJavaConfigFile() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("createJavaConfigFile() got = %v, want %v", got, tt.want)
			}
			if got1 != tt.want1 {
				t.Errorf("createJavaConfigFile() got1 = %v, want %v", got1, tt.want1)
			}
		})
	}
}

func Test_getTestsSelection(t *testing.T) {
	type args struct {
		ctx         context.Context
		fs          filesystem.FileSystem
		stepID      string
		workspace   string
		log         *logrus.Logger
		isManual    bool
		tiConfig    *tiCfg.Cfg
		envs        map[string]string
		runV2Config *api.RunTestsV2Config
	}
	tests := []struct {
		name  string
		args  args
		want  types.SelectTestsResp
		want1 bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, got1 := getTestsSelection(tt.args.ctx, tt.args.fs, tt.args.stepID, tt.args.workspace, tt.args.log, tt.args.isManual, tt.args.tiConfig, tt.args.envs, tt.args.runV2Config)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("getTestsSelection() got = %v, want %v", got, tt.want)
			}
			if got1 != tt.want1 {
				t.Errorf("getTestsSelection() got1 = %v, want %v", got1, tt.want1)
			}
		})
	}
}
