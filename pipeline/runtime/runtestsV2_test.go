package runtime

import (
	"context"
	"fmt"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/harness/lite-engine/api"
	"github.com/harness/lite-engine/internal/filesystem"
	tiCfg "github.com/harness/lite-engine/ti/config"
	"github.com/harness/ti-client/types"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"

	tiClient "github.com/harness/ti-client/client"
)

func Test_CollectRunTestsV2Data(t *testing.T) {
	ctx := context.Background()
	log := logrus.New()

	apiReq := api.StartStepRequest{}
	stepName := "RunTestsV2"
	tiConfig := tiCfg.New("app.harness.io", "", "", "", "", "",
		"", "", "", "", "", "", "", "",
		"", false, false, "", "")

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
			collectCgFn = func(ctx context.Context, stepID string, timeMs int64, log *logrus.Logger, start time.Time, cfg *tiCfg.Cfg, dir string, uniqueStepID string, tests []*types.TestCase, r *api.StartStepRequest) error {
				return tc.cgErr
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
			err := collectTestReportsAndCg(ctx, log, &apiReq, time.Now(), stepName, &tiConfig, &types.TelemetryData{}, map[string]string{})
			assert.Equal(t, tc.collectionErr, err)
		})
	}
}

func Test_createSelectedTestFile(t *testing.T) {
	type args struct {
		ctx                      context.Context
		fs                       filesystem.FileSystem
		stepID                   string
		workspace                string
		log                      *logrus.Logger
		tiConfig                 *tiCfg.Cfg
		tmpFilepath              string
		envs                     map[string]string
		runV2Config              *api.RunTestsV2Config
		filterFilePath           string
		testIntelligenceMetaData *types.TestIntelligenceMetaData
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
			if err := createSelectedTestFile(tt.args.ctx,
				tt.args.fs,
				tt.args.stepID,
				tt.args.workspace,
				tt.args.log,
				tt.args.tiConfig,
				tt.args.tmpFilepath,
				tt.args.envs,
				tt.args.runV2Config,
				tt.args.filterFilePath,
				tt.args.testIntelligenceMetaData); (err != nil) != tt.wantErr {
				t.Errorf("createSelectedTestFile() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func Test_downloadJavaAgent(t *testing.T) {
	type args struct {
		ctx      context.Context
		path     string
		agentURL string
		fs       filesystem.FileSystem
		log      *logrus.Logger
		client   tiClient.Client
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
			if err := downloadJavaAgent(tt.args.ctx, tt.args.path, tt.args.agentURL, tt.args.fs, tt.args.log, tt.args.client); (err != nil) != tt.wantErr {
				t.Errorf("downloadJavaAgent() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func Test_getPreCmd(t *testing.T) {
	type args struct {
		workspace   string
		tmpFilePath string
		fs          filesystem.FileSystem
		tiConfig    *tiCfg.Cfg
		log         *logrus.Logger
		envs        map[string]string
		agentPaths  map[string]string
	}
	tests := []struct {
		name    string
		args    args
		want    string
		want1   string
		want2   string
		want3   string
		wantErr bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, got1, got2, got3, err := getPreCmd(tt.args.workspace, tt.args.tmpFilePath, tt.args.fs, tt.args.log, tt.args.envs, tt.args.agentPaths, false, tt.args.tiConfig)
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
			if got2 != tt.want2 {
				t.Errorf("getPreCmd() got2 = %v, want %v", got2, tt.want2)
			}
			if got3 != tt.want3 {
				t.Errorf("getPreCmd() got3 = %v, want %v", got3, tt.want3)
			}
		})
	}
}

func Test_createJavaConfigFile(t *testing.T) {
	type args struct {
		tmpDir         string
		fs             filesystem.FileSystem
		filterFilePath string
		outDir         string
		log            *logrus.Logger
		splitIdx       int
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
			got, err := createJavaConfigFile(tt.args.tmpDir, tt.args.fs, tt.args.log, tt.args.filterFilePath, tt.args.outDir, tt.args.splitIdx)
			if (err != nil) != tt.wantErr {
				t.Errorf("createJavaConfigFile() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("createJavaConfigFile() got = %v, want %v", got, tt.want)
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

func Test_writetoBazelrcFile(t *testing.T) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return
	}
	t.Run("testPath", func(t *testing.T) {
		err := writetoBazelrcFile(logrus.New(), filesystem.New())
		if err != nil {
			t.Errorf("writetoBazelrcFile() error = %v, wantErr %v", err, homeDir+"/.bazelrc")
			return
		}
	})
}

func TestSanitizeTestGlobsV2(t *testing.T) {
	tests := []struct {
		name        string
		globStrings []string
		expected    []string
	}{
		{
			name:        "Empty Input",
			globStrings: []string{},
			expected:    []string{},
		},
		{
			name:        "Single Glob",
			globStrings: []string{"*.txt"},
			expected:    []string{"*.txt"},
		},
		{
			name:        "Multiple Globs",
			globStrings: []string{"*.txt,*.md,*.pdf"},
			expected:    []string{"*.txt", "*.md", "*.pdf"},
		},
		{
			name:        "Empty String in Input",
			globStrings: []string{"", "*.txt,*.md,*.pdf"},
			expected:    []string{"*.txt", "*.md", "*.pdf"},
		},
		{
			name:        "Empty Globs in Input",
			globStrings: []string{"", ""},
			expected:    []string{},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := sanitizeTestGlobsV2(test.globStrings)
			if !reflect.DeepEqual(result, test.expected) {
				t.Errorf("Test case %s failed. Expected: %v, Got: %v", test.name, test.expected, result)
			}
		})
	}
}
