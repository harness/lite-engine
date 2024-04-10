package instrumentation

import (
	"context"
	"testing"

	"github.com/golang/mock/gomock"
	tiCfg "github.com/harness/lite-engine/ti/config"
	ti "github.com/harness/ti-client/types"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

func Test_GetSplitTests(t *testing.T) {
	log := logrus.New()
	ctrl, ctx := gomock.WithContext(context.Background(), t)
	defer ctrl.Finish()

	stepID := "RunTestStep"
	prevBuildID := ""

	tiConfig := tiCfg.New("app.harness.io", "", "", "", "", "",
		"", "", "", "", "", "", "", "",
		"", false, false)

	testsToSplit := []ti.RunnableTest{
		{Pkg: "pkg1", Class: "cls1"},
		{Pkg: "pkg1", Class: "cls2"},
		{Pkg: "pkg2", Class: "cls1"},
		{Pkg: "pkg2", Class: "cls2"},
		{Pkg: "pkg3", Class: "cls1"},
	}
	splitStrategy := countTestSplitStrategy
	splitTotal := 3
	tests, _ := getSplitTests(ctx, log, testsToSplit, stepID, prevBuildID, splitStrategy, 0, splitTotal, &tiConfig)
	assert.Equal(t, len(tests), 2)
	tests, _ = getSplitTests(ctx, log, testsToSplit, stepID, prevBuildID, splitStrategy, 1, splitTotal, &tiConfig)
	assert.Equal(t, len(tests), 2)
	tests, _ = getSplitTests(ctx, log, testsToSplit, stepID, prevBuildID, splitStrategy, 2, splitTotal, &tiConfig)
	assert.Equal(t, len(tests), 1)
}
