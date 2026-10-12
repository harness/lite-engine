package runtime

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/harness/lite-engine/api"
	"github.com/harness/lite-engine/internal/filesystem"
	"github.com/harness/lite-engine/ti/callgraph"
	tiCfg "github.com/harness/lite-engine/ti/config"
	"github.com/harness/lite-engine/ti/instrumentation"
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
		"", "", false, "", "")

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
				_ *tiCfg.Cfg, _, _ string, _ []*types.TestCase, _ bool, _ *api.StartStepRequest,
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
			_, err := collectTestReportsAndCg(ctx, log, &apiReq, time.Now(), stepName, &tiConfig, &types.TelemetryData{}, map[string]string{})
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
			if _, err := createSelectedTestFile(tt.args.ctx,
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
	// The agent only emits the git-relative file paths the enhanced flow matches on when
	// sourceFilesNonNative and ignoreMethodLevel are set, so they must appear under the flag
	// and stay absent without it.
	tests := []struct {
		name        string
		envs        map[string]string
		wantEnhance bool
	}{
		{
			name:        "EnhancedFFDisabled",
			envs:        map[string]string{},
			wantEnhance: false,
		},
		{
			name:        "EnhancedFFEnabled",
			envs:        map[string]string{"CI_TI_V2_ENHANCED_FF": "true"},
			wantEnhance: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			iniFile, err := createJavaConfigFile(tmpDir, filesystem.New(), logrus.New(), "/filter/filter_0", "/out/", 0, tt.envs)
			assert.Nil(t, err)

			contents, err := os.ReadFile(iniFile)
			assert.Nil(t, err)
			config := string(contents)

			assert.Contains(t, config, "packageInference: true")
			assert.Contains(t, config, "filterFile: /filter/filter_0")
			if tt.wantEnhance {
				assert.Contains(t, config, "sourceFilesNonNative: true")
				assert.Contains(t, config, "ignoreMethodLevel: true")
			} else {
				assert.NotContains(t, config, "sourceFilesNonNative")
				assert.NotContains(t, config, "ignoreMethodLevel")
			}
		})
	}
}

func Test_MatrixParallelismShardPaths(t *testing.T) {
	// In Group B (Stage matrix + step parallelism), the second matrix leg (e.g. Greeter, stageIdx=1)
	// must evaluate splitIdx to 0 and 1, ensuring skip and filter file paths match the local step shard
	// (skip_tests_0, config_0.ini) rather than a global index (skip_tests_2, config_2.ini).
	tests := []struct {
		name          string
		envs          map[string]string
		wantSplitIdx  int
		wantSkipFile  string
		wantFilter    string
		wantFailed    string
		wantConfigIni string
	}{
		{
			name: "GroupB_Greeter_Shard0",
			envs: map[string]string{
				"HARNESS_MATRIX_AXIS": `{"project":"greeter"}`,
				"HARNESS_STAGE_INDEX": "1",
				"HARNESS_STAGE_TOTAL": "2",
				"HARNESS_STEP_INDEX":  "0",
				"HARNESS_STEP_TOTAL":  "2",
			},
			wantSplitIdx:  0,
			wantSkipFile:  "skip_tests_0",
			wantFilter:    "filter_0",
			wantFailed:    "failed_tests_0",
			wantConfigIni: "config_0.ini",
		},
		{
			name: "GroupB_Greeter_Shard1",
			envs: map[string]string{
				"HARNESS_MATRIX_AXIS": `{"project":"greeter"}`,
				"HARNESS_STAGE_INDEX": "1",
				"HARNESS_STAGE_TOTAL": "2",
				"HARNESS_STEP_INDEX":  "1",
				"HARNESS_STEP_TOTAL":  "2",
			},
			wantSplitIdx:  1,
			wantSkipFile:  "skip_tests_1",
			wantFilter:    "filter_1",
			wantFailed:    "failed_tests_1",
			wantConfigIni: "config_1.ini",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			splitIdx := 0
			if instrumentation.IsParallelismActive(tt.envs) {
				splitIdx, _ = instrumentation.GetSplitIdxAndTotalWithMatrix(tt.envs)
			}
			assert.Equal(t, tt.wantSplitIdx, splitIdx)

			tmpDir := t.TempDir()
			filterPath := getFilterFilePath(tmpDir, splitIdx)
			skipPath := getSkipTestsFilePath(tmpDir, splitIdx)
			failedPath := getFailedTestsFilePath(tmpDir, splitIdx)
			iniPath, err := createJavaConfigFile(tmpDir, filesystem.New(), logrus.New(), filterPath, "/out", splitIdx, tt.envs)
			assert.Nil(t, err)

			assert.True(t, strings.HasSuffix(filterPath, tt.wantFilter), "filterPath must end with %s, got %s", tt.wantFilter, filterPath)
			assert.True(t, strings.HasSuffix(skipPath, tt.wantSkipFile), "skipPath must end with %s, got %s", tt.wantSkipFile, skipPath)
			assert.True(t, strings.HasSuffix(failedPath, tt.wantFailed), "failedPath must end with %s, got %s", tt.wantFailed, failedPath)
			assert.True(t, strings.HasSuffix(iniPath, tt.wantConfigIni), "iniPath must end with %s, got %s", tt.wantConfigIni, iniPath)
		})
	}
}

func Test_SetupRunTestV2_SkipsAgentWhenTIAndParallelismDisabled(t *testing.T) {
	config := &api.RunTestsV2Config{
		IntelligenceMode: false,
	}
	tiConfig := tiCfg.New("app.harness.io", "", "", "", "", "",
		"", "", "", "", "", "", "", "",
		"", "", false, "", "")

	envs := map[string]string{} // no parallelism
	preCmd, selectedCounts, err := setupRunTestV2(
		context.Background(), config, "step1", t.TempDir(), "unique1",
		logrus.New(), envs, &tiConfig, &types.TestIntelligenceMetaData{},
	)
	assert.Nil(t, err)
	assert.Empty(t, preCmd)
	assert.True(t, selectedCounts)
}

func Test_CreateSkipAndFailedTestsFiles_WhenIntelligenceModeOff(t *testing.T) {
	workspace := t.TempDir()
	fs := filesystem.New()
	log := logrus.New()

	// Create test files in workspace
	testFile1 := filepath.Join(workspace, "src/test/java/Test1.java")
	testFile2 := filepath.Join(workspace, "src/test/java/Test2.java")
	assert.Nil(t, fs.MkdirAll(filepath.Dir(testFile1), os.ModePerm))
	assert.Nil(t, os.WriteFile(testFile1, []byte("class Test1 {}"), 0644))
	assert.Nil(t, os.WriteFile(testFile2, []byte("class Test2 {}"), 0644))

	config := &api.RunTestsV2Config{
		IntelligenceMode: false,
		TestGlobs:        []string{"**/*Test*.java"},
	}

	tiConfig := tiCfg.New("app.harness.io", "", "", "", "", "",
		"", "", "", "", "", "", "", "",
		"", "", false, "", "")

	tmpDir := t.TempDir()
	skipFile0 := filepath.Join(tmpDir, "filter/skip_tests_0")
	failedFile0 := filepath.Join(tmpDir, "filter/failed_tests_0")

	envs0 := map[string]string{
		"HARNESS_STEP_INDEX": "0",
		"HARNESS_STEP_TOTAL": "2",
	}

	err := createSkipAndFailedTestsFiles(context.Background(), fs, "step1", workspace, log, &tiConfig, skipFile0, failedFile0, envs0, config)
	assert.Nil(t, err)

	content0, err := os.ReadFile(skipFile0)
	assert.Nil(t, err)
	lines0 := strings.Split(strings.TrimSpace(string(content0)), "\n")
	assert.Equal(t, 1, len(lines0), "Shard 0 should skip exactly 1 test file")

	skipFile1 := filepath.Join(tmpDir, "filter/skip_tests_1")
	failedFile1 := filepath.Join(tmpDir, "filter/failed_tests_1")
	envs1 := map[string]string{
		"HARNESS_STEP_INDEX": "1",
		"HARNESS_STEP_TOTAL": "2",
	}

	err = createSkipAndFailedTestsFiles(context.Background(), fs, "step1", workspace, log, &tiConfig, skipFile1, failedFile1, envs1, config)
	assert.Nil(t, err)

	content1, err := os.ReadFile(skipFile1)
	assert.Nil(t, err)
	lines1 := strings.Split(strings.TrimSpace(string(content1)), "\n")
	assert.Equal(t, 1, len(lines1), "Shard 1 should skip exactly 1 test file")

	// Shard 0 and Shard 1 must skip different files (partitioning the tests)
	assert.NotEqual(t, lines0[0], lines1[0], "Shards must skip complementary test files")
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

// CI-22586: enhanced intelligence selects by a skip list and does not populate
// selected counts before report collection. Executed report totals are authoritative.
func TestBackfillRunTestsV2SelectedTelemetry_IntelligenceOn_FirstRunAllSelected(t *testing.T) {
	telemetryData := &types.TelemetryData{}
	telemetryData.TestIntelligenceMetaData.TotalTests = 5
	telemetryData.TestIntelligenceMetaData.TotalTestClasses = 2

	backfillRunTestsV2SelectedTelemetry(telemetryData, true)

	assert.True(t, telemetryData.TestIntelligenceMetaData.IsRunTestV2)
	assert.Equal(t, 5, telemetryData.TestIntelligenceMetaData.TotalSelectedTests)
	assert.Equal(t, 2, telemetryData.TestIntelligenceMetaData.TotalSelectedTestClass)
}

func TestBackfillRunTestsV2SelectedTelemetry_IntelligenceOff_AllSelected(t *testing.T) {
	telemetryData := &types.TelemetryData{}
	telemetryData.TestIntelligenceMetaData.TotalTests = 5
	telemetryData.TestIntelligenceMetaData.TotalTestClasses = 2

	backfillRunTestsV2SelectedTelemetry(telemetryData, true)

	assert.True(t, telemetryData.TestIntelligenceMetaData.IsRunTestV2)
	assert.Equal(t, 5, telemetryData.TestIntelligenceMetaData.TotalSelectedTests)
	assert.Equal(t, 2, telemetryData.TestIntelligenceMetaData.TotalSelectedTestClass)
}

func TestBackfillRunTestsV2SelectedTelemetry_PreservesExistingSelected(t *testing.T) {
	telemetryData := &types.TelemetryData{}
	telemetryData.TestIntelligenceMetaData.TotalTests = 10
	telemetryData.TestIntelligenceMetaData.TotalTestClasses = 4
	telemetryData.TestIntelligenceMetaData.TotalSelectedTests = 3
	telemetryData.TestIntelligenceMetaData.TotalSelectedTestClass = 1

	backfillRunTestsV2SelectedTelemetry(telemetryData, true)

	assert.Equal(t, 3, telemetryData.TestIntelligenceMetaData.TotalSelectedTests)
	assert.Equal(t, 1, telemetryData.TestIntelligenceMetaData.TotalSelectedTestClass)
}

// A selection response that explicitly chose zero tests must remain zero, even if
// report totals are present (for example, stale or unrelated report files).
func TestBackfillRunTestsV2SelectedTelemetry_IntelligenceOn_SubsequentRunZeroSelected(t *testing.T) {
	telemetryData := &types.TelemetryData{}
	telemetryData.TestIntelligenceMetaData.TotalTests = 5
	telemetryData.TestIntelligenceMetaData.TotalTestClasses = 2

	backfillRunTestsV2SelectedTelemetry(telemetryData, false)

	assert.True(t, telemetryData.TestIntelligenceMetaData.IsRunTestV2)
	assert.Equal(t, 0, telemetryData.TestIntelligenceMetaData.TotalSelectedTests)
	assert.Equal(t, 0, telemetryData.TestIntelligenceMetaData.TotalSelectedTestClass)
}

func TestBackfillRunTestsV2SelectedTelemetry_NilSafe(t *testing.T) {
	assert.NotPanics(t, func() {
		backfillRunTestsV2SelectedTelemetry(nil, true)
	})
}
