package instrumentation

import (
	"context"
	"fmt"
	"strconv"
	"testing"

	"github.com/harness/lite-engine/api"
	tiCfg "github.com/harness/lite-engine/ti/config"
	mocks "github.com/harness/lite-engine/ti/instrumentation/mocks"
	ti "github.com/harness/ti-client/types"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"go.uber.org/mock/gomock"
)

func TestComputeSelected(t *testing.T) { //nolint:funlen
	log := logrus.New()

	rts := make([]ti.RunnableTest, 0)
	for i := 1; i <= 12; i++ {
		rt := ti.RunnableTest{
			Pkg:   fmt.Sprintf("p%d", i),
			Class: fmt.Sprintf("c%d", i),
		}
		rts = append(rts, rt)
	}
	tests := []struct {
		name string
		// Input
		runOnlySelectedTestsBool bool
		IgnoreInstrBool          bool
		parallelizeTestsBool     bool
		stepStrategyIteration    int
		stepStrategyIterations   int
		stageStrategyIteration   int
		stageStrategyIterations  int
		runnableTests            []ti.RunnableTest
		runnerAutodetectExpect   bool
		runnerAutodetectTestsVal []ti.RunnableTest
		runnerAutodetectTestsErr error
		// Verify
		runOnlySelectedTests     bool
		selectTestsResponseTests []ti.RunnableTest
	}{
		{
			name: "SkipParallelization_Manual",
			// Input
			runOnlySelectedTestsBool: true,
			IgnoreInstrBool:          false,
			parallelizeTestsBool:     false,
			// Expect
			runOnlySelectedTests: true,
		},
		{
			name: "SkipParallelization_TiSelection",
			// Input
			runOnlySelectedTestsBool: true,
			IgnoreInstrBool:          false,
			parallelizeTestsBool:     false,
			runnableTests:            rts[:1],
			// Expect
			runOnlySelectedTests:     true,
			selectTestsResponseTests: rts[:1],
		},
		{
			name: "SkipTestSplitting_TiSelectionZeroTests",
			// Input
			runOnlySelectedTestsBool: true,
			IgnoreInstrBool:          false,
			parallelizeTestsBool:     true,
			runnableTests:            []ti.RunnableTest{}, // TI returned 0 tests to run
			// Expect
			runOnlySelectedTests:     true,
			selectTestsResponseTests: []ti.RunnableTest{},
		},
		{
			name: "ManualAutodetectPass",
			// Input
			runOnlySelectedTestsBool: false,
			IgnoreInstrBool:          true,
			parallelizeTestsBool:     true,
			stepStrategyIteration:    0,
			stepStrategyIterations:   2,
			stageStrategyIteration:   -1,
			stageStrategyIterations:  -1,
			runnableTests:            []ti.RunnableTest{}, // Manual run - No TI test selection
			runnerAutodetectExpect:   true,
			runnerAutodetectTestsVal: []ti.RunnableTest{rts[0], rts[1]},
			runnerAutodetectTestsErr: nil,
			// Expect
			runOnlySelectedTests:     true,
			selectTestsResponseTests: []ti.RunnableTest{rts[0]},
		},
		{
			name: "ManualAutodetectFailStepZero",
			// Input
			runOnlySelectedTestsBool: false,
			IgnoreInstrBool:          true,
			parallelizeTestsBool:     true,
			stepStrategyIteration:    0,
			stepStrategyIterations:   2,
			stageStrategyIteration:   -1,
			stageStrategyIterations:  -1,
			runnableTests:            []ti.RunnableTest{}, // Manual run - No TI test selection
			runnerAutodetectExpect:   true,
			runnerAutodetectTestsVal: []ti.RunnableTest{},
			runnerAutodetectTestsErr: fmt.Errorf("error in autodetection"),
			// Expect
			runOnlySelectedTests:     false,
			selectTestsResponseTests: []ti.RunnableTest{},
		},
		{
			name: "ManualAutodetectFailStepNonZero",
			// Input
			runOnlySelectedTestsBool: false,
			IgnoreInstrBool:          true,
			parallelizeTestsBool:     true,
			stepStrategyIteration:    1,
			stepStrategyIterations:   2,
			stageStrategyIteration:   -1,
			stageStrategyIterations:  -1,
			runnableTests:            []ti.RunnableTest{}, // Manual run - No TI test selection
			runnerAutodetectExpect:   true,
			runnerAutodetectTestsVal: []ti.RunnableTest{},
			runnerAutodetectTestsErr: fmt.Errorf("error in autodetection"),
			// Expect
			runOnlySelectedTests:     true,
			selectTestsResponseTests: make([]ti.RunnableTest, 0),
		},
		{
			name: "TestParallelism_StageParallelismOnly",
			// Input
			runOnlySelectedTestsBool: true,
			parallelizeTestsBool:     true,
			stepStrategyIteration:    0,
			stepStrategyIterations:   2,
			stageStrategyIteration:   -1,
			stageStrategyIterations:  -1,
			runnableTests:            []ti.RunnableTest{rts[0], rts[1]}, // t1, t2
			// Expect
			runOnlySelectedTests:     true,
			selectTestsResponseTests: []ti.RunnableTest{rts[0]}, // (Stage 0, Step) - t1
		},
		{
			name: "TestParallelism_StepParallelismOnly",
			// Input
			runOnlySelectedTestsBool: true,
			parallelizeTestsBool:     true,
			stepStrategyIteration:    -1,
			stepStrategyIterations:   -1,
			stageStrategyIteration:   0,
			stageStrategyIterations:  2,
			runnableTests:            []ti.RunnableTest{rts[0], rts[1]}, // t1, t2
			// Expect
			runOnlySelectedTests:     true,
			selectTestsResponseTests: []ti.RunnableTest{rts[0]}, // (Stage, Step 1) - t2
		},
		{
			name: "TestParallelism_StageStepParallelism_v1",
			// Input
			runOnlySelectedTestsBool: true,
			parallelizeTestsBool:     true,
			stepStrategyIteration:    1,
			stepStrategyIterations:   2,
			stageStrategyIteration:   0,
			stageStrategyIterations:  2,
			runnableTests:            []ti.RunnableTest{rts[0], rts[1], rts[2], rts[3]}, // t1, t2, t3, t4
			// Expect
			runOnlySelectedTests:     true,
			selectTestsResponseTests: []ti.RunnableTest{rts[1]}, // (Stage 0, Step 1) - t2
		},
		{
			name: "TestParallelism_StageStepParallelism_v2",
			// Input
			runOnlySelectedTestsBool: true,
			parallelizeTestsBool:     true,
			stepStrategyIteration:    1,
			stepStrategyIterations:   2,
			stageStrategyIteration:   1,
			stageStrategyIterations:  2,
			runnableTests:            []ti.RunnableTest{rts[0], rts[1], rts[2], rts[3]}, // t1, t2, t3, t4
			// Expect
			runOnlySelectedTests:     true,
			selectTestsResponseTests: []ti.RunnableTest{rts[3]}, // (Stage 1, Step 1) - t4
		},
		{
			name: "TestParallelism_StageStepParallelism_v30",
			// Input
			runOnlySelectedTestsBool: true,
			parallelizeTestsBool:     true,
			stepStrategyIteration:    0,
			stepStrategyIterations:   2,
			stageStrategyIteration:   0,
			stageStrategyIterations:  3,
			runnableTests:            rts[:6], // t1, t2, t3, t4, t5, t6
			// Expect
			runOnlySelectedTests:     true,
			selectTestsResponseTests: []ti.RunnableTest{rts[0]}, // (Stage 0, Step 0) - t1
		},
		{
			name: "TestParallelism_StageStepParallelism_v31",
			// Input
			runOnlySelectedTestsBool: true,
			parallelizeTestsBool:     true,
			stepStrategyIteration:    1,
			stepStrategyIterations:   2,
			stageStrategyIteration:   0,
			stageStrategyIterations:  3,
			runnableTests:            rts[:6], // t1, t2, t3, t4, t5, t6
			// Expect
			runOnlySelectedTests:     true,
			selectTestsResponseTests: []ti.RunnableTest{rts[1]}, // (Stage 0, Step 1) - t2
		},
		{
			name: "TestParallelism_StageStepParallelism_v32",
			// Input
			runOnlySelectedTestsBool: true,
			parallelizeTestsBool:     true,
			stepStrategyIteration:    0,
			stepStrategyIterations:   2,
			stageStrategyIteration:   1,
			stageStrategyIterations:  3,
			runnableTests:            rts[:6], // t1, t2, t3, t4, t5, t6
			// Expect
			runOnlySelectedTests:     true,
			selectTestsResponseTests: []ti.RunnableTest{rts[2]}, // (Stage 1, Step 0) - t3
		},
		{
			name: "TestParallelism_StageStepParallelism_v33",
			// Input
			runOnlySelectedTestsBool: true,
			parallelizeTestsBool:     true,
			stepStrategyIteration:    1,
			stepStrategyIterations:   2,
			stageStrategyIteration:   1,
			stageStrategyIterations:  3,
			runnableTests:            rts[:6], // t1, t2, t3, t4, t5, t6
			// Expect
			runOnlySelectedTests:     true,
			selectTestsResponseTests: []ti.RunnableTest{rts[3]}, // (Stage 1, Step 1) - t4
		},
		{
			name: "TestParallelism_StageStepParallelism_v34",
			// Input
			runOnlySelectedTestsBool: true,
			parallelizeTestsBool:     true,
			stepStrategyIteration:    0,
			stepStrategyIterations:   2,
			stageStrategyIteration:   2,
			stageStrategyIterations:  3,
			runnableTests:            rts[:6], // t1, t2, t3, t4, t5, t6
			// Expect
			runOnlySelectedTests:     true,
			selectTestsResponseTests: []ti.RunnableTest{rts[4]}, // (Stage 2, Step 0) - t5
		},
		{
			name: "TestParallelism_StageStepParallelism_v35",
			// Input
			runOnlySelectedTestsBool: true,
			parallelizeTestsBool:     true,
			stepStrategyIteration:    1,
			stepStrategyIterations:   2,
			stageStrategyIteration:   2,
			stageStrategyIterations:  3,
			runnableTests:            rts[:6], // t1, t2, t3, t4, t5, t6
			// Expect
			runOnlySelectedTests:     true,
			selectTestsResponseTests: []ti.RunnableTest{rts[5]}, // (Stage 2, Step 1) - t5
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl, ctx := gomock.WithContext(context.Background(), t)
			defer ctrl.Finish()

			envs := map[string]string{
				harnessStepIndex:  strconv.Itoa(tt.stepStrategyIteration),
				harnessStepTotal:  strconv.Itoa(tt.stepStrategyIterations),
				harnessStageIndex: strconv.Itoa(tt.stageStrategyIteration),
				harnessStageTotal: strconv.Itoa(tt.stageStrategyIterations),
			}
			config := &api.RunTestConfig{
				Args:                 "test",
				PreCommand:           "echo x",
				BuildTool:            "maven",
				Language:             "java",
				Packages:             "io.harness",
				RunOnlySelectedTests: tt.runOnlySelectedTestsBool,
				TestSplitStrategy:    countTestSplitStrategy,
				ParallelizeTests:     tt.parallelizeTestsBool,
			}
			runner := mocks.NewMockTestRunner(ctrl)
			testGlobs := make([]string, 0)
			if tt.runnerAutodetectExpect {
				runner.EXPECT().AutoDetectTests(ctx, "", testGlobs).Return(tt.runnerAutodetectTestsVal, tt.runnerAutodetectTestsErr)
			}

			stepID := "RunTestsStep"

			selectTestsResponse := ti.SelectTestsResp{}
			selectTestsResponse.Tests = tt.runnableTests
			tiConfig := tiCfg.New("app.harness.io", "", "", "", "", "",
				"", "", "", "", "", "", "", "",
				"", "", false, false, "", "")

			computeSelectedTests(ctx, config, log, runner, &selectTestsResponse, stepID, "", envs, &tiConfig)

			assert.Equal(t, config.RunOnlySelectedTests, tt.runOnlySelectedTests)
			assert.Equal(t, selectTestsResponse.Tests, tt.selectTestsResponseTests)
		})
	}
}

func TestFilterSelection(t *testing.T) {
	testGlobStr := ""
	testGlob := sanitizeTestGlob(testGlobStr)
	rts := make([]ti.RunnableTest, 0)
	for i := 1; i <= 12; i++ {
		rt := ti.RunnableTest{
			Pkg:   fmt.Sprintf("p%d", i),
			Class: fmt.Sprintf("c%d", i),
		}
		rts = append(rts, rt)
	}
	rt := ti.RunnableTest{
		Pkg:   "package",
		Class: "vendor/test.rb",
	}
	rt2 := ti.RunnableTest{
		Pkg:   "package",
		Class: "harness/vendor/test.rb",
	}
	rts2 := append(rts, rt) //nolint:gocritic
	rts2 = append(rts2, rt2)
	selection := ti.SelectTestsResp{
		TotalTests:    20,
		SelectedTests: 12,
		NewTests:      0,
		UpdatedTests:  12,
		SrcCodeTests:  12,
		SelectAll:     false,
		Tests:         rts2,
	}

	filteredTests := filterTestsAfterSelection(selection, testGlob, make([]string, 0))
	assert.Equal(t, filteredTests.Tests, rts2)

	testGlob = []string{"abc"}
	selection = ti.SelectTestsResp{
		TotalTests:    20,
		SelectedTests: 12,
		NewTests:      0,
		UpdatedTests:  12,
		SrcCodeTests:  12,
		SelectAll:     true,
		Tests:         rts2,
	}

	filteredTests = filterTestsAfterSelection(selection, testGlob, make([]string, 0))
	assert.Equal(t, filteredTests.Tests, rts2)

	testGlob = []string{"c1"}
	selection = ti.SelectTestsResp{
		TotalTests:    20,
		SelectedTests: 12,
		NewTests:      0,
		UpdatedTests:  12,
		SrcCodeTests:  12,
		SelectAll:     false,
		Tests:         rts2,
	}

	filteredTests = filterTestsAfterSelection(selection, testGlob, make([]string, 0))
	assert.Equal(t, filteredTests.Tests, []ti.RunnableTest{rts[0]})

	testGlob = []string{"c*"}
	selection = ti.SelectTestsResp{
		TotalTests:    20,
		SelectedTests: 12,
		NewTests:      0,
		UpdatedTests:  12,
		SrcCodeTests:  12,
		SelectAll:     false,
		Tests:         rts2,
	}
	filteredTests = filterTestsAfterSelection(selection, testGlob, make([]string, 0))
	assert.Equal(t, filteredTests.Tests, rts)

	testGlob = []string{"**/*"}
	selection = ti.SelectTestsResp{
		TotalTests:    20,
		SelectedTests: 12,
		NewTests:      0,
		UpdatedTests:  12,
		SrcCodeTests:  12,
		SelectAll:     false,
		Tests:         rts2,
	}

	filteredTests = filterTestsAfterSelection(selection, testGlob, []string{"**/vendor/**/*.rb"})
	assert.Equal(t, filteredTests.Tests, rts)
}
