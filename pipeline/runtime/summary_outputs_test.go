package runtime

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	runnerRuntime "github.com/drone/runner-go/pipeline/runtime"
	"github.com/harness/lite-engine/api"
	"github.com/harness/lite-engine/engine/spec"
	"github.com/harness/lite-engine/pipeline"
	"github.com/harness/lite-engine/ti/callgraph"
	tiCfg "github.com/harness/lite-engine/ti/config"
	"github.com/harness/ti-client/types"
	"github.com/sirupsen/logrus"
)

// The test summary exposed as step outputs is computed from the reports the step produced and
// must never be requested from the TI service. Drives Run, RunTests and RunTestsV2 end to end.

const summaryTestReport = `<testsuites><testsuite name="s">
<testcase classname="c" name="passes" time="0.1"/>
<testcase classname="c" name="fails" time="0.2"><failure message="boom"/></testcase>
<testcase classname="c" name="errors" time="0.3"><error message="npe"/></testcase>
<testcase classname="c" name="skipped" time="0"><skipped/></testcase>
</testsuite></testsuites>`

var wantSummary = map[string]string{
	"total_tests": "4", "successful_tests": "1", "failed_tests": "2", "skipped_tests": "1", "duration_ms": "600",
}

func runSummaryStepKind(t *testing.T, kind string, flagOn bool, exitCode int, outputVars []string, fileOutputs string) (map[string]string, map[string]string) {
	t.Helper()
	workDir := t.TempDir()
	t.Setenv("HARNESS_WORKDIR", workDir)
	if err := os.MkdirAll(pipeline.GetSharedVolPath(), 0o755); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/reports/summary") {
			t.Errorf("the summary must be computed locally, but the TI summary endpoint was called: %s", r.URL)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	cfg := tiCfg.New(server.URL, "tok", "acc", "org", "proj", "pipe", "1", "stage", "repo", "sha", "", "", "", "", "", "", false, "", "")

	reportPath := filepath.Join(workDir, "report.xml")
	if err := os.WriteFile(reportPath, []byte(summaryTestReport), 0o600); err != nil {
		t.Fatal(err)
	}
	envs := map[string]string{}
	if flagOn {
		envs["HARNESS_CI_TEST_SUMMARY_OUTPUT_FF"] = "true"
	}
	req := &api.StartStepRequest{ID: "id-" + kind, Name: "step-" + kind, Envs: envs, WorkingDir: workDir, OutputVars: outputVars}
	req.TestReport.Kind = api.Junit
	req.TestReport.Junit.Paths = []string{reportPath}
	outputFile := fmt.Sprintf("%s/%s-output.env", pipeline.GetSharedVolPath(), req.ID)
	switch kind {
	case "Run":
		req.Run = api.RunConfig{Entrypoint: []string{"sh", "-c"}, Command: []string{"true"}}
		req.OutputVarFile = outputFile
	case "RunTests":
		req.RunTest = api.RunTestConfig{Language: "java", BuildTool: "maven", Entrypoint: []string{"sh", "-c"}, Args: "test"}
	case "RunTestsV2":
		req.RunTestsV2 = api.RunTestsV2Config{Entrypoint: []string{"sh", "-c"}, Command: []string{"true"}}
	}
	f := func(_ context.Context, _ *spec.Step, _ io.Writer, _, _ bool) (*runnerRuntime.State, error) {
		if fileOutputs != "" {
			if err := os.WriteFile(outputFile, []byte(fileOutputs), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		return &runnerRuntime.State{Exited: true, ExitCode: exitCode}, nil
	}
	oldCg := collectCgFn
	collectCgFn = func(context.Context, string, int64, *logrus.Logger, time.Time, *tiCfg.Cfg, string, string, []*types.TestCase, bool, *api.StartStepRequest) (*callgraph.Callgraph, error) {
		return nil, nil
	}
	defer func() { collectCgFn = oldCg }()

	var legacy map[string]string
	var v2 []*api.OutputV2
	buf := &bytes.Buffer{}
	switch kind {
	case "Run":
		_, legacy, _, _, v2, _, _, _ = executeRunStep(context.Background(), f, req, buf, &cfg)
	case "RunTests":
		_, legacy, _, _, v2, _, _, _ = executeRunTestStep(context.Background(), f, req, buf, &cfg)
	case "RunTestsV2":
		_, legacy, _, _, v2, _, _, _ = executeRunTestsV2Step(context.Background(), f, req, buf, &cfg)
	}
	v2m := map[string]string{}
	for _, o := range v2 {
		v2m[o.Key] = o.Value
	}
	return legacy, v2m
}

func TestSummaryOutputsComeFromLocalReports(t *testing.T) {
	for _, kind := range []string{"Run", "RunTests", "RunTestsV2"} {
		for _, exit := range []int{0, 1} {
			for name, outputVars := range map[string][]string{"no outputs": nil, "output vars": {"version"}} {
				t.Run(fmt.Sprintf("%s/exit%d/%s", kind, exit, name), func(t *testing.T) {
					file := ""
					if outputVars != nil {
						file = "version=1.2\n"
					}
					_, v2 := runSummaryStepKind(t, kind, true, exit, outputVars, file)
					for k, want := range wantSummary {
						if v2[k] != want {
							t.Errorf("%s = %q, want %q (outputs: %v)", k, v2[k], want, v2)
						}
					}
				})
			}
		}
	}
}

func TestSummaryOutputsOffByDefault(t *testing.T) {
	for _, kind := range []string{"Run", "RunTests", "RunTestsV2"} {
		for _, exit := range []int{0, 1} {
			legacy, v2 := runSummaryStepKind(t, kind, false, exit, []string{"version"}, "version=1.2\n")
			for k := range wantSummary {
				if _, ok := legacy[k]; ok {
					t.Errorf("%s exit %d: %s in legacy outputs with the flag off", kind, exit, k)
				}
				if _, ok := v2[k]; ok {
					t.Errorf("%s exit %d: %s in V2 outputs with the flag off", kind, exit, k)
				}
			}
		}
	}
}
