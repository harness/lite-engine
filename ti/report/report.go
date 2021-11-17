package report

import (
	"context"
	"fmt"
	"strings"

	"github.com/harness/lite-engine/api"
	"github.com/harness/lite-engine/pipeline"
	"github.com/harness/lite-engine/ti/client"
	"github.com/harness/lite-engine/ti/report/parser/junit"
)

func ParseAndUploadTests(ctx context.Context, report api.TestReport, stepID string) error {
	if report.Kind != api.Junit {
		return fmt.Errorf("unknown report type: %s", report.Kind)
	}

	if len(report.Junit.Paths) == 0 {
		return nil
	}

	tests := junit.ParseTests(report.Junit.Paths)
	if len(tests) == 0 {
		return nil
	}

	config := pipeline.GetState().GetTIConfig()
	if config == nil || config.URL == "" {
		return fmt.Errorf("TI config is not provided in setup")
	}

	c := client.NewHTTPClient(config.URL, config.Token, config.AccountID, config.OrgID, config.ProjectID,
		config.PipelineID, config.BuildID, config.StageID, config.Repo, config.Sha, false)
	return c.Write(ctx, stepID, strings.ToLower(report.Kind.String()), tests)
}
