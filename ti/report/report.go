// Copyright 2022 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package report

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/harness/lite-engine/api"
	tiCfg "github.com/harness/lite-engine/ti/config"
	"github.com/harness/lite-engine/ti/report/parser/junit"
	"github.com/sirupsen/logrus"
)

func ParseAndUploadTests(ctx context.Context, report api.TestReport, workDir, stepID string, log *logrus.Logger, start time.Time, tiConfig *tiCfg.Cfg) error {
	return ParseAndUploadTestsForLanguage(ctx, report, workDir, stepID, "", log, start, tiConfig)
}

func ParseAndUploadTestsForLanguage(ctx context.Context, report api.TestReport, workDir, stepID, language string, log *logrus.Logger, start time.Time, tiConfig *tiCfg.Cfg) error {
	switch strings.ToLower(language) {
	case "python", "ruby":
		if len(report.Junit.Paths) == 0 {
			report.Junit.Paths = []string{"harness_test_results.xml*"}
		} else {
			if report.Kind != api.Junit {
				return fmt.Errorf("unknown report type: %s", report.Kind)
			}
		}
	default:
		if report.Kind != api.Junit {
			return fmt.Errorf("unknown report type: %s", report.Kind)
		}

		if len(report.Junit.Paths) == 0 {
			return nil
		}
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

	tests := junit.ParseTests(report.Junit.Paths, log)
	if len(tests) == 0 {
		return nil
	}

	startTime := time.Now()
	logrus.WithContext(ctx).Infoln(fmt.Sprintf("Starting TI service request to write report for step %s", stepID))
	c := tiConfig.GetClient()
	if err := c.Write(ctx, stepID, strings.ToLower(report.Kind.String()), tests); err != nil {
		return err
	}
	logrus.WithContext(ctx).Infoln(fmt.Sprintf("Completed TI service request to write report for step %s, took %.2f seconds", stepID, time.Since(startTime).Seconds()))
	log.Infoln(fmt.Sprintf("Successfully collected test reports in %s time", time.Since(start)))
	return nil
}
