// Copyright 2022 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package report

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/harness/lite-engine/api"
	tiCfg "github.com/harness/lite-engine/ti/config"
	"github.com/harness/lite-engine/ti/report/parser/junit"
	telemetryutils "github.com/harness/ti-client/clientUtils/telemetryUtils"
	"github.com/harness/ti-client/types"
	"github.com/sirupsen/logrus"
)

func ParseAndUploadTests(
	ctx context.Context,
	report api.TestReport,
	workDir, stepID string,
	log *logrus.Logger,
	start time.Time,
	tiConfig *tiCfg.Cfg,
	testMetadata *types.TestIntelligenceMetaData,
	envs map[string]string,
) ([]*types.TestCase, error) {
	if report.Kind != api.Junit {
		return nil, fmt.Errorf("unknown report type: %s", report.Kind)
	}

	if len(report.Junit.Paths) == 0 {
		return []*types.TestCase{}, nil
	}

	// Append working dir to the paths. In k8s, we specify the workDir in the YAML but this is
	// needed in case of VMs.
	for idx, p := range report.Junit.Paths {
		if p[0] != '~' && p[0] != '/' && p[0] != '\\' {
			if !strings.HasPrefix(p, workDir) {
				report.Junit.Paths[idx] = filepath.Join(workDir, p)
			}
		}
	}

	tests := junit.ParseTests(report.Junit.Paths, log, envs)
	if len(tests) == 0 {
		return tests, nil
	}

	startTime := time.Now()
	logrus.WithContext(ctx).Infoln(fmt.Sprintf("Starting TI service request to write report for step %s", stepID))
	c := tiConfig.GetClient()
	if err := c.Write(ctx, stepID, strings.ToLower(report.Kind.String()), tests); err != nil {
		return nil, err
	}
	logrus.WithContext(ctx).Infoln(fmt.Sprintf("Completed TI service request to write report for step %s, took %.2f seconds", stepID, time.Since(startTime).Seconds()))
	// Write tests telemetry data, total test, total test classes, selected test, selected classes,
	testMetadata.TotalTests = len(tests)
	testMetadata.TotalTestClasses = telemetryutils.CountDistinctClasses(tests)
	log.Infoln(fmt.Sprintf("Successfully collected test reports in %s time", time.Since(start)))
	return tests, nil
}

func SaveReportSummaryToOutputs(ctx context.Context, tiConfig *tiCfg.Cfg, stepID string, outputs map[string]string, log *logrus.Logger, envs map[string]string) error {
	if !TestSummaryAsOutputEnabled(envs) {
		return nil
	}
	tiClient := tiConfig.GetClient()
	summaryRequest := types.SummaryRequest{
		AllStages:  false,
		OrgID:      tiConfig.GetOrgID(),
		ProjectID:  tiConfig.GetProjectID(),
		PipelineID: tiConfig.GetPipelineID(),
		BuildID:    tiConfig.GetBuildID(),
		StageID:    tiConfig.GetStageID(),
		StepID:     stepID,
		ReportType: "junit",
	}
	response, err := tiClient.Summary(ctx, summaryRequest)
	if err != nil {
		return err
	}
	if response.TotalTests == 0 {
		return errors.New("no tests found in the summary")
	}
	outputs["total_tests"] = fmt.Sprintf("%d", response.TotalTests)
	outputs["successful_tests"] = fmt.Sprintf("%d", response.SuccessfulTests)
	outputs["failed_tests"] = fmt.Sprintf("%d", response.FailedTests)
	outputs["skipped_tests"] = fmt.Sprintf("%d", response.SkippedTests)
	outputs["duration_ms"] = fmt.Sprintf("%d", response.TimeMs)
	return nil
}

func GetSummaryOutputsV2(outputs, envs map[string]string) []*api.OutputV2 {
	outputsV2 := []*api.OutputV2{}
	if !TestSummaryAsOutputEnabled(envs) {
		return outputsV2
	}
	outputsV2 = checkAndAddSummary("total_tests", outputs, outputsV2)
	outputsV2 = checkAndAddSummary("successful_tests", outputs, outputsV2)
	outputsV2 = checkAndAddSummary("failed_tests", outputs, outputsV2)
	outputsV2 = checkAndAddSummary("skipped_tests", outputs, outputsV2)
	outputsV2 = checkAndAddSummary("duration_ms", outputs, outputsV2)
	return outputsV2
}

func checkAndAddSummary(metricName string, outputs map[string]string, outputsV2 []*api.OutputV2) []*api.OutputV2 {
	if _, ok := outputs[metricName]; ok {
		outputsV2 = append(outputsV2, &api.OutputV2{
			Key:   metricName,
			Value: outputs[metricName],
			Type:  api.OutputTypeString,
		})
	}
	return outputsV2
}

func TestSummaryAsOutputEnabled(envs map[string]string) bool {
	value, present := envs["HARNESS_CI_TEST_SUMMARY_OUTPUT_FF"]
	if !present {
		return false
	}
	return value == "true"
}
