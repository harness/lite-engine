package report

import (
	"testing"

	"github.com/harness/ti-client/types"
)

func tc(status types.Status, durationMs int64) *types.TestCase {
	return &types.TestCase{Result: types.Result{Status: status}, DurationMs: durationMs}
}

func enabledEnvs() map[string]string {
	return map[string]string{"HARNESS_CI_TEST_SUMMARY_OUTPUT_FF": "true"}
}

func TestSaveReportSummaryToOutputs_Counts(t *testing.T) {
	tests := []*types.TestCase{
		tc(types.StatusPassed, 100),
		tc(types.StatusPassed, 50),
		tc(types.StatusFailed, 25),
		tc(types.StatusError, 25), // errors count as failed, like the TI service does
		tc(types.StatusSkipped, 0),
		nil,
	}
	outputs := map[string]string{}
	if err := SaveReportSummaryToOutputs(tests, outputs, enabledEnvs()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := map[string]string{
		"total_tests":      "5",
		"successful_tests": "2",
		"failed_tests":     "2",
		"skipped_tests":    "1",
		"duration_ms":      "200",
	}
	if len(outputs) != len(want) {
		t.Fatalf("outputs = %v, want %v", outputs, want)
	}
	for k, v := range want {
		if outputs[k] != v {
			t.Errorf("%s: got %q, want %q", k, outputs[k], v)
		}
	}
}

func TestSaveReportSummaryToOutputs_AllFailed(t *testing.T) {
	outputs := map[string]string{}
	if err := SaveReportSummaryToOutputs([]*types.TestCase{tc(types.StatusFailed, 10), tc(types.StatusFailed, 5)}, outputs, enabledEnvs()); err != nil {
		t.Fatal(err)
	}
	if outputs["total_tests"] != "2" || outputs["failed_tests"] != "2" || outputs["successful_tests"] != "0" || outputs["skipped_tests"] != "0" || outputs["duration_ms"] != "15" {
		t.Errorf("unexpected outputs: %v", outputs)
	}
}

// Unknown statuses are part of the total only, as they are in the TI service's summary.
func TestSaveReportSummaryToOutputs_UnknownStatusOnlyInTotal(t *testing.T) {
	outputs := map[string]string{}
	if err := SaveReportSummaryToOutputs([]*types.TestCase{tc(types.Status("weird"), 1), tc(types.StatusPassed, 1)}, outputs, enabledEnvs()); err != nil {
		t.Fatal(err)
	}
	if outputs["total_tests"] != "2" || outputs["successful_tests"] != "1" || outputs["failed_tests"] != "0" || outputs["skipped_tests"] != "0" {
		t.Errorf("unexpected outputs: %v", outputs)
	}
}

func TestSaveReportSummaryToOutputs_NoTests(t *testing.T) {
	for name, tests := range map[string][]*types.TestCase{"nil": nil, "empty": {}, "only nil entries": {nil, nil}} {
		outputs := map[string]string{}
		if err := SaveReportSummaryToOutputs(tests, outputs, enabledEnvs()); err == nil {
			t.Errorf("%s: expected an error", name)
		}
		if len(outputs) != 0 {
			t.Errorf("%s: outputs must stay empty, got %v", name, outputs)
		}
	}
}

func TestSaveReportSummaryToOutputs_FeatureOff(t *testing.T) {
	for _, envs := range []map[string]string{nil, {}, {"HARNESS_CI_TEST_SUMMARY_OUTPUT_FF": "false"}, {"HARNESS_CI_TEST_SUMMARY_OUTPUT_FF": "TRUE"}} {
		outputs := map[string]string{}
		if err := SaveReportSummaryToOutputs([]*types.TestCase{tc(types.StatusPassed, 1)}, outputs, envs); err != nil || len(outputs) != 0 {
			t.Errorf("envs %v: err=%v outputs=%v", envs, err, outputs)
		}
	}
}
