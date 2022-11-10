package instrumentation

import (
	"context"
	"github.com/golang/mock/gomock"
	"github.com/harness/lite-engine/ti"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"testing"
)

func Test_GetSplitTests(t *testing.T) {
	log := logrus.New()
	ctrl, ctx := gomock.WithContext(context.Background(), t)
	defer ctrl.Finish()

	testsToSplit := []ti.RunnableTest{
		{Pkg: "pkg1", Class: "cls1"},
		{Pkg: "pkg1", Class: "cls2"},
		{Pkg: "pkg2", Class: "cls1"},
		{Pkg: "pkg2", Class: "cls2"},
		{Pkg: "pkg3", Class: "cls1"},
	}
	splitStrategy := countTestSplitStrategy
	splitTotal := 3
	tests, _ := getSplitTests(ctx, log, testsToSplit, splitStrategy, 0, splitTotal)
	assert.Equal(t, len(tests), 2)
	tests, _ = getSplitTests(ctx, log, testsToSplit, splitStrategy, 1, splitTotal)
	assert.Equal(t, len(tests), 2)
	tests, _ = getSplitTests(ctx, log, testsToSplit, splitStrategy, 2, splitTotal)
	assert.Equal(t, len(tests), 1)
}
