package instrumentation

import (
	"context"
	"reflect"
	"testing"

	tiCfg "github.com/harness/lite-engine/ti/config"
	ti "github.com/harness/ti-client/types"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"go.uber.org/mock/gomock"
)

func Test_GetSplitTests(t *testing.T) {
	log := logrus.New()
	ctrl, ctx := gomock.WithContext(context.Background(), t)
	defer ctrl.Finish()

	stepID := "RunTestStep"

	tiConfig := tiCfg.New("app.harness.io", "", "", "", "", "",
		"", "", "", "", "", "", "", "",
		"", "", false, "", "")

	testsToSplit := []ti.RunnableTest{
		{Pkg: "pkg1", Class: "cls1"},
		{Pkg: "pkg1", Class: "cls2"},
		{Pkg: "pkg2", Class: "cls1"},
		{Pkg: "pkg2", Class: "cls2"},
		{Pkg: "pkg3", Class: "cls1"},
	}
	splitStrategy := countTestSplitStrategy
	splitTotal := 3
	tests, _ := getSplitTests(ctx, log, testsToSplit, stepID, splitStrategy, 0, splitTotal, &tiConfig)
	assert.Equal(t, len(tests), 2)
	tests, _ = getSplitTests(ctx, log, testsToSplit, stepID, splitStrategy, 1, splitTotal, &tiConfig)
	assert.Equal(t, len(tests), 2)
	tests, _ = getSplitTests(ctx, log, testsToSplit, stepID, splitStrategy, 2, splitTotal, &tiConfig)
	assert.Equal(t, len(tests), 1)
}

func TestGetV2AgentDownloadLinks(t *testing.T) {
	type args struct {
		ctx    context.Context
		config *tiCfg.Cfg
	}
	tests := []struct {
		name    string
		args    args
		want    []ti.DownloadLink
		wantErr bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := GetV2AgentDownloadLinks(tt.args.ctx, tt.args.config, false)
			if (err != nil) != tt.wantErr {
				t.Errorf("GetV2AgentDownloadLinks() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("GetV2AgentDownloadLinks() = %v, want %v", got, tt.want)
			}
		})
	}
}

type splitIdxMatrixCase struct {
	name           string
	envs           map[string]string
	wantSplitIdx   int
	wantSplitTotal int
	wantActive     bool
}

var splitIdxAndTotalWithMatrixTestCases = []splitIdxMatrixCase{
	{
		name: "NoParallelism",
		envs: map[string]string{
			"HARNESS_STEP_INDEX":  "0",
			"HARNESS_STEP_TOTAL":  "1",
			"HARNESS_STAGE_INDEX": "0",
			"HARNESS_STAGE_TOTAL": "1",
		},
		wantSplitIdx:   0,
		wantSplitTotal: 1,
		wantActive:     false,
	},
	{
		name: "StepParallelismOnly_Shard0",
		envs: map[string]string{
			"HARNESS_STEP_INDEX":  "0",
			"HARNESS_STEP_TOTAL":  "2",
			"HARNESS_STAGE_INDEX": "0",
			"HARNESS_STAGE_TOTAL": "1",
		},
		wantSplitIdx:   0,
		wantSplitTotal: 2,
		wantActive:     true,
	},
	{
		name: "StepParallelismOnly_Shard1",
		envs: map[string]string{
			"HARNESS_STEP_INDEX":  "1",
			"HARNESS_STEP_TOTAL":  "2",
			"HARNESS_STAGE_INDEX": "0",
			"HARNESS_STAGE_TOTAL": "1",
		},
		wantSplitIdx:   1,
		wantSplitTotal: 2,
		wantActive:     true,
	},
	{
		name: "StageMatrixOnly_NoStepParallelism_Leg0",
		envs: map[string]string{
			"HARNESS_MATRIX_AXIS": `{"project":"calculator"}`,
			"HARNESS_STAGE_INDEX": "0",
			"HARNESS_STAGE_TOTAL": "2",
			"HARNESS_STEP_INDEX":  "0",
			"HARNESS_STEP_TOTAL":  "1",
		},
		wantSplitIdx:   0,
		wantSplitTotal: 1,
		wantActive:     false,
	},
	{
		name: "StageMatrixOnly_NoStepParallelism_Leg1",
		envs: map[string]string{
			"HARNESS_MATRIX_AXIS": `{"project":"greeter"}`,
			"HARNESS_STAGE_INDEX": "1",
			"HARNESS_STAGE_TOTAL": "2",
			"HARNESS_STEP_INDEX":  "0",
			"HARNESS_STEP_TOTAL":  "1",
		},
		wantSplitIdx:   0,
		wantSplitTotal: 1,
		wantActive:     false,
	},
	{
		name: "StageMatrixOnly_NestedObjectAxis_NoStepParallelism",
		envs: map[string]string{
			"HARNESS_MATRIX_AXIS": `{"container":{"module":"string-service:test","dockerfile":"","name":"string-service","source_path":"string-service"}}`,
			"HARNESS_STAGE_INDEX": "0",
			"HARNESS_STAGE_TOTAL": "2",
			"HARNESS_STEP_INDEX":  "0",
			"HARNESS_STEP_TOTAL":  "1",
		},
		wantSplitIdx:   0,
		wantSplitTotal: 1,
		wantActive:     false,
	},
	{
		name: "StageMatrix_NestedObjectAxis_WithStepParallelism_Shard1",
		envs: map[string]string{
			"HARNESS_MATRIX_AXIS": `{"container":{"module":"string-service:test","dockerfile":"","name":"string-service","source_path":"string-service"}}`,
			"HARNESS_STAGE_INDEX": "0",
			"HARNESS_STAGE_TOTAL": "2",
			"HARNESS_STEP_INDEX":  "1",
			"HARNESS_STEP_TOTAL":  "2",
		},
		wantSplitIdx:   1,
		wantSplitTotal: 2,
		wantActive:     true,
	},
	{
		name: "GroupB_MatrixWithStepParallelism_Calculator_Shard0",
		envs: map[string]string{
			"HARNESS_MATRIX_AXIS": `{"project":"calculator"}`,
			"HARNESS_STAGE_INDEX": "0",
			"HARNESS_STAGE_TOTAL": "2",
			"HARNESS_STEP_INDEX":  "0",
			"HARNESS_STEP_TOTAL":  "2",
		},
		wantSplitIdx:   0,
		wantSplitTotal: 2,
		wantActive:     true,
	},
	{
		name: "GroupB_MatrixWithStepParallelism_Calculator_Shard1",
		envs: map[string]string{
			"HARNESS_MATRIX_AXIS": `{"project":"calculator"}`,
			"HARNESS_STAGE_INDEX": "0",
			"HARNESS_STAGE_TOTAL": "2",
			"HARNESS_STEP_INDEX":  "1",
			"HARNESS_STEP_TOTAL":  "2",
		},
		wantSplitIdx:   1,
		wantSplitTotal: 2,
		wantActive:     true,
	},
	{
		name: "GroupB_MatrixWithStepParallelism_Greeter_Shard0",
		envs: map[string]string{
			"HARNESS_MATRIX_AXIS": `{"project":"greeter"}`,
			"HARNESS_STAGE_INDEX": "1",
			"HARNESS_STAGE_TOTAL": "2",
			"HARNESS_STEP_INDEX":  "0",
			"HARNESS_STEP_TOTAL":  "2",
		},
		// Shard index discrepancy fix: Must be 0 of 2, NOT 2 of 4!
		wantSplitIdx:   0,
		wantSplitTotal: 2,
		wantActive:     true,
	},
	{
		name: "GroupB_MatrixWithStepParallelism_Greeter_Shard1",
		envs: map[string]string{
			"HARNESS_MATRIX_AXIS": `{"project":"greeter"}`,
			"HARNESS_STAGE_INDEX": "1",
			"HARNESS_STAGE_TOTAL": "2",
			"HARNESS_STEP_INDEX":  "1",
			"HARNESS_STEP_TOTAL":  "2",
		},
		// Shard index discrepancy fix: Must be 1 of 2, NOT 3 of 4!
		wantSplitIdx:   1,
		wantSplitTotal: 2,
		wantActive:     true,
	},
	{
		name: "StageParallelismWithoutMatrix_Stage0",
		envs: map[string]string{
			"HARNESS_STAGE_INDEX": "0",
			"HARNESS_STAGE_TOTAL": "2",
			"HARNESS_STEP_INDEX":  "0",
			"HARNESS_STEP_TOTAL":  "1",
		},
		wantSplitIdx:   0,
		wantSplitTotal: 2,
		wantActive:     true,
	},
	{
		name: "StageParallelismWithoutMatrix_Stage1",
		envs: map[string]string{
			"HARNESS_STAGE_INDEX": "1",
			"HARNESS_STAGE_TOTAL": "2",
			"HARNESS_STEP_INDEX":  "0",
			"HARNESS_STEP_TOTAL":  "1",
		},
		wantSplitIdx:   1,
		wantSplitTotal: 2,
		wantActive:     true,
	},
}

func TestGetSplitIdxAndTotalWithMatrix(t *testing.T) {
	for _, tt := range splitIdxAndTotalWithMatrixTestCases {
		t.Run(tt.name, func(t *testing.T) {
			gotIdx, gotTotal := GetSplitIdxAndTotalWithMatrix(tt.envs)
			assert.Equal(t, tt.wantSplitIdx, gotIdx, "splitIdx mismatch")
			assert.Equal(t, tt.wantSplitTotal, gotTotal, "splitTotal mismatch")

			gotActive := IsParallelismActive(tt.envs)
			assert.Equal(t, tt.wantActive, gotActive, "IsParallelismActive mismatch")
		})
	}
}

func TestShouldSkipAgentInfrastructure(t *testing.T) {
	tests := []struct {
		name             string
		intelligenceMode bool
		envs             map[string]string
		wantSkip         bool
	}{
		{
			name:             "TI_Off_No_Parallelism",
			intelligenceMode: false,
			envs:             map[string]string{},
			wantSkip:         true,
		},
		{
			name:             "TI_On_No_Parallelism",
			intelligenceMode: true,
			envs:             map[string]string{},
			wantSkip:         false,
		},
		{
			name:             "TI_Off_Step_Parallelism",
			intelligenceMode: false,
			envs: map[string]string{
				"HARNESS_STEP_INDEX": "0",
				"HARNESS_STEP_TOTAL": "2",
			},
			wantSkip: false, // parallelism alone requires agent infrastructure
		},
		{
			name:             "TI_On_Step_Parallelism",
			intelligenceMode: true,
			envs: map[string]string{
				"HARNESS_STEP_INDEX": "0",
				"HARNESS_STEP_TOTAL": "2",
			},
			wantSkip: false,
		},
		{
			name:             "TI_Off_StageMatrix_Without_StepParallelism",
			intelligenceMode: false,
			envs: map[string]string{
				"HARNESS_STAGE_INDEX": "0",
				"HARNESS_STAGE_TOTAL": "2",
				"HARNESS_MATRIX_AXIS": `{"project":"calculator"}`,
			},
			wantSkip: true, // stage matrix legs without step parallelism do not split tests
		},
		{
			name:             "TI_Off_StageMatrix_With_StepParallelism",
			intelligenceMode: false,
			envs: map[string]string{
				"HARNESS_STAGE_INDEX": "0",
				"HARNESS_STAGE_TOTAL": "2",
				"HARNESS_STEP_INDEX":  "0",
				"HARNESS_STEP_TOTAL":  "2",
				"HARNESS_MATRIX_AXIS": `{"project":"calculator"}`,
			},
			wantSkip: false, // step parallelism inside matrix requires agents for test splitting
		},
		{
			name:             "TI_Off_StageParallelism_WithoutMatrix",
			intelligenceMode: false,
			envs: map[string]string{
				"HARNESS_STAGE_INDEX": "0",
				"HARNESS_STAGE_TOTAL": "2",
			},
			wantSkip: false, // stage-level test splitting requires agents even when TI is off
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ShouldSkipAgentInfrastructure(tt.intelligenceMode, tt.envs)
			assert.Equal(t, tt.wantSkip, got)
		})
	}
}

func TestReadMatrixAxisString(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "empty", input: "", want: ""},
		{name: "invalid json", input: "not-json", want: ""},
		{name: "non object json", input: `["a","b"]`, want: ""},
		{name: "trailing garbage", input: `{"a":"b"}garbage`, want: ""},
		{name: "trailing second value", input: `{"a":"b"} {"c":"d"}`, want: ""},
		{name: "trailing whitespace", input: "{\"a\":\"b\"} \n", want: "a=b"},
		{name: "empty object", input: `{}`, want: ""},
		{name: "string values sorted by key", input: `{"project":"calculator","jdk":"17"}`, want: "jdk=17,project=calculator"},
		{
			name:  "object value",
			input: `{"container":{"module":"string-service:test","dockerfile":"","name":"string-service","source_path":"string-service"}}`,
			want:  `container={"dockerfile":"","module":"string-service:test","name":"string-service","source_path":"string-service"}`,
		},
		{
			name:  "object value is independent of key order",
			input: `{"container":{"source_path":"string-service","name":"string-service","dockerfile":"","module":"string-service:test"}}`,
			want:  `container={"dockerfile":"","module":"string-service:test","name":"string-service","source_path":"string-service"}`,
		},
		{
			name:  "mixed value types",
			input: `{"os":"linux","shards":4,"big":12345678901234567890,"enabled":true,"tags":["a","b"],"opt":null}`,
			want:  `big=12345678901234567890,enabled=true,opt=null,os=linux,shards=4,tags=["a","b"]`,
		},
		{name: "html characters not escaped", input: `{"cfg":{"cmd":"a<b&c>d"}}`, want: `cfg={"cmd":"a<b&c>d"}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := readMatrixAxisString(tt.input); got != tt.want {
				t.Fatalf("readMatrixAxisString(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestIsMatrixEnabledStage(t *testing.T) {
	tests := []struct {
		name string
		envs map[string]string
		want bool
	}{
		{name: "env not set", envs: map[string]string{}, want: false},
		{name: "invalid json", envs: map[string]string{envHarnessMatrixAxis: "not-json"}, want: false},
		{name: "trailing garbage", envs: map[string]string{envHarnessMatrixAxis: `{"a":"b"}garbage`}, want: false},
		{name: "empty object", envs: map[string]string{envHarnessMatrixAxis: `{}`}, want: false},
		{name: "string values", envs: map[string]string{envHarnessMatrixAxis: `{"project":"calculator"}`}, want: true},
		{
			name: "object value",
			envs: map[string]string{envHarnessMatrixAxis: `{"container":{"module":"string-service:test","dockerfile":"","name":"string-service","source_path":"string-service"}}`},
			want: true,
		},
		{name: "non string scalar values", envs: map[string]string{envHarnessMatrixAxis: `{"shards":4,"enabled":true}`}, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsMatrixEnabledStage(tt.envs); got != tt.want {
				t.Fatalf("IsMatrixEnabledStage() = %v, want %v", got, tt.want)
			}
		})
	}
}
