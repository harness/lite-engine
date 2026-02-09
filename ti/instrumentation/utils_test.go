package instrumentation

import (
	"context"
	"reflect"
	"testing"

	tiCfg "github.com/harness/lite-engine/ti/config"
	ti "github.com/harness/ti-client/types"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"go.uber.org/mock/gomock"
)

func Test_GetSplitTests(t *testing.T) {
	log := logrus.New()
	ctrl, ctx := gomock.WithContext(context.Background(), t)
	defer ctrl.Finish()

	stepID := "RunTestStep"

	tiConfig := tiCfg.New("app.harness.io", "", "", "", "", "",
		"", "", "", "", "", "", "", "",
		"", "", false, false, "", "")

	testsToSplit := []ti.RunnableTest{
		{Pkg: "pkg1", Class: "cls1"},
		{Pkg: "pkg1", Class: "cls2"},
		{Pkg: "pkg2", Class: "cls1"},
		{Pkg: "pkg2", Class: "cls2"},
		{Pkg: "pkg3", Class: "cls1"},
	}
	splitStrategy := countTestSplitStrategy
	splitTotal := 3
	tests, _ := getSplitTests(ctx, log, testsToSplit, stepID, splitStrategy, 0, splitTotal, &tiConfig)
	assert.Equal(t, len(tests), 2)
	tests, _ = getSplitTests(ctx, log, testsToSplit, stepID, splitStrategy, 1, splitTotal, &tiConfig)
	assert.Equal(t, len(tests), 2)
	tests, _ = getSplitTests(ctx, log, testsToSplit, stepID, splitStrategy, 2, splitTotal, &tiConfig)
	assert.Equal(t, len(tests), 1)
}

func TestGetV2AgentDownloadLinks(t *testing.T) {
	type args struct {
		ctx    context.Context
		config *tiCfg.Cfg
	}
	tests := []struct {
		name    string
		args    args
		want    []ti.DownloadLink
		wantErr bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := GetV2AgentDownloadLinks(tt.args.ctx, tt.args.config, false)
			if (err != nil) != tt.wantErr {
				t.Errorf("GetV2AgentDownloadLinks() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("GetV2AgentDownloadLinks() = %v, want %v", got, tt.want)
			}
		})
	}
}
