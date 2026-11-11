//go:build tiservice_e2e

package report

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/harness/lite-engine/api"
	tiCfg "github.com/harness/lite-engine/ti/config"
	"github.com/harness/ti-client/types"
	"github.com/sirupsen/logrus"
)

// Runs against a real ti-service (TI_SERVICE_URL): the summary computed from the parsed tests must
// equal the summary the service computes from the very same upload.
func TestLocalSummaryMatchesTIService(t *testing.T) {
	url := os.Getenv("TI_SERVICE_URL")
	if url == "" {
		t.Skip("TI_SERVICE_URL not set")
	}
	cases := map[string]string{
		"all_pass":    `<testsuites><testsuite name="s"><testcase classname="c" name="a" time="1.5"/><testcase classname="c" name="b" time="2"/></testsuite></testsuites>`,
		"all_fail":    `<testsuites><testsuite name="s"><testcase classname="c" name="a" time="1"><failure message="x"/></testcase><testcase classname="c" name="b" time="1"><error message="y"/></testcase></testsuite></testsuites>`,
		"mixed":       `<testsuites><testsuite name="s"><testcase classname="c" name="a" time="1"/><testcase classname="c" name="b" time="1"><failure message="x"/></testcase><testcase classname="c" name="c" time="0"><skipped/></testcase><testcase classname="c" name="d" time="2"><error message="y"/></testcase></testsuite></testsuites>`,
		"duplicates":  `<testsuites><testsuite name="s"><testcase classname="c" name="a" time="1"/><testcase classname="c" name="a" time="1"/></testsuite></testsuites>`,
		"multi_suite": `<testsuites><testsuite name="s1"><testcase classname="c" name="a" time="1"/></testsuite><testsuite name="s2"><testcase classname="c" name="a" time="1"><failure message="x"/></testcase><testcase classname="c" name="z" time="1"><skipped/></testcase></testsuite></testsuites>`,
	}
	// real-world reports from the parser's test data, including a TRX file
	for _, f := range []string{"agent-skip.trx", "reportWithNestedTestSuite.xml", "reportWithPassFail.xml", "reportWithSkipError.xml"} {
		b, err := os.ReadFile(filepath.Join("parser", "junit", "testdata", f))
		if err != nil {
			t.Fatal(err)
		}
		cases[f] = string(b)
	}
	for name, xml := range cases {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			p := filepath.Join(dir, "r.xml")
			if strings.HasSuffix(name, ".trx") {
				p = filepath.Join(dir, "r.trx")
			}
			if err := os.WriteFile(p, []byte(xml), 0o600); err != nil {
				t.Fatal(err)
			}
			// ids must be unique per run: the service keeps what earlier runs uploaded
			id := strings.NewReplacer(".", "_").Replace(name) + "-" + strconv.FormatInt(time.Now().UnixNano(), 36)
			cfg := tiCfg.New(url, "tok", "acc", "org", "proj", "pipe", "1", "stage-"+id, "repo", "sha", "", "", "", "", "", "", false, "", "")
			tr := api.TestReport{Kind: api.Junit}
			tr.Junit.Paths = []string{p}
			tests, err := ParseAndUploadTests(context.Background(), tr, dir, "step-"+id, logrus.New(), time.Now(), &cfg, &types.TestIntelligenceMetaData{}, nil)
			if err != nil {
				t.Fatal(err)
			}
			local := map[string]string{}
			if err := SaveReportSummaryToOutputs(tests, local, enabledEnvs()); err != nil {
				t.Fatal(err)
			}
			srv, err := cfg.GetClient().Summary(context.Background(), types.SummaryRequest{
				OrgID: "org", ProjectID: "proj", PipelineID: "pipe", BuildID: "1", StageID: "stage-" + id, StepID: "step-" + id, ReportType: "junit"})
			if err != nil {
				t.Fatal(err)
			}
			remote := map[string]string{
				"total_tests": strconv.Itoa(srv.TotalTests), "successful_tests": strconv.Itoa(srv.SuccessfulTests),
				"failed_tests": strconv.Itoa(srv.FailedTests), "skipped_tests": strconv.Itoa(srv.SkippedTests),
				"duration_ms": strconv.FormatInt(srv.TimeMs, 10),
			}
			for k, v := range remote {
				if local[k] != v {
					t.Errorf("%s: local=%s ti-service=%s", k, local[k], v)
				}
			}
		})
	}
}
