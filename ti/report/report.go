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
	"github.com/harness/lite-engine/pipeline"
	"github.com/harness/lite-engine/ti/client"
	"github.com/harness/lite-engine/ti/report/parser/junit"
	"github.com/sirupsen/logrus"
)

func ParseAndUploadTests(ctx context.Context, report api.TestReport, workDir, stepID string, log *logrus.Logger, start time.Time) error {
	if report.Kind != api.Junit {
		return fmt.Errorf("unknown report type: %s", report.Kind)
	}

	if len(report.Junit.Paths) == 0 {
		return nil
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

	config := pipeline.GetState().GetTIConfig()
	if config == nil || config.URL == "" {
		return fmt.Errorf("TI config is not provided in setup")
	}

	c := client.NewHTTPClient(config.URL, config.Token, config.AccountID, config.OrgID, config.ProjectID,
		config.PipelineID, config.BuildID, config.StageID, config.Repo, config.Sha, config.CommitLink, false)
	if err := c.Write(ctx, stepID, strings.ToLower(report.Kind.String()), tests); err != nil {
		return err
	}
	log.Infoln(fmt.Sprintf("Successfully collected test reports in %s time", time.Since(start)))
	return nil
}
