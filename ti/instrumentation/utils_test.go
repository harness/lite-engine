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

func TestGetSplitIdxAndTotalWithMatrix(t *testing.T) {
	tests := []struct {
		name           string
		envs           map[string]string
		wantSplitIdx   int
		wantSplitTotal int
		wantActive     bool
	}{
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

	for _, tt := range tests {
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
