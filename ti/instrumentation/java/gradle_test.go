// Copyright 2022 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package java

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/harness/lite-engine/internal/filesystem"
	"github.com/harness/lite-engine/ti/instrumentation/common"
	ti "github.com/harness/ti-client/types"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"go.uber.org/mock/gomock"
)

func TestGetGradleCmd(t *testing.T) { //nolint:funlen
	ctrl, ctx := gomock.WithContext(context.Background(), t)
	defer ctrl.Finish()

	log := logrus.New()
	fs := filesystem.NewMockFileSystem(ctrl)

	fs.EXPECT().Stat("/path/to/workspace/gradlew").Return(nil, nil).AnyTimes()

	runner := NewGradleRunner(log, fs)
	installDir := "/install/dir/java/"
	jarPath := filepath.Join(installDir, JavaAgentJar)
	agent := fmt.Sprintf(AgentArg, jarPath, "/test/tmp/config.ini")

	t1 := ti.RunnableTest{Pkg: "pkg1", Class: "cls1", Method: "m1"}
	t2 := ti.RunnableTest{Pkg: "pkg2", Class: "cls2", Method: "m2"}

	tests := []struct {
		name                 string // description of test
		args                 string
		runOnlySelectedTests bool
		ignoreInstr          bool
		want                 string
		expectedErr          bool
		tests                []ti.RunnableTest
	}{
		// PR Run
		{
			name:                 "RunAllTests_AgentAttached",
			args:                 "test",
			runOnlySelectedTests: false,
			ignoreInstr:          false,
			want:                 fmt.Sprintf("./gradlew test -DHARNESS_JAVA_AGENT=%s", agent),
			expectedErr:          false,
			tests:                []ti.RunnableTest{t1, t2},
		},
		{
			name:                 "RunSelectedTests_TwoTests_UserParams_AgentAttached",
			args:                 "test -Duser.timezone=US/Mountain",
			runOnlySelectedTests: true,
			ignoreInstr:          false,
			want:                 fmt.Sprintf("./gradlew test -Duser.timezone=US/Mountain -DHARNESS_JAVA_AGENT=%s --tests \"pkg1.cls1\" --tests \"pkg2.cls2\"", agent),
			expectedErr:          false,
			tests:                []ti.RunnableTest{t1, t2},
		},
		{
			name:                 "RunSelectedTests_ZeroTests_UserParams_AgentAttached",
			args:                 "test -Duser.timezone=US/Mountain -Duser.locale=en/US",
			runOnlySelectedTests: true,
			ignoreInstr:          false,
			want:                 "echo \"Skipping test run, received no tests to execute\"",
			expectedErr:          false,
			tests:                []ti.RunnableTest{},
		},
		{
			name:                 "RunSelectedTests_TwoTests__Duplicate_UserParams_AgentAttached",
			args:                 "test -Duser.timezone=US/Mountain -Duser.locale=en/US",
			runOnlySelectedTests: true,
			ignoreInstr:          false,
			want:                 fmt.Sprintf("./gradlew test -Duser.timezone=US/Mountain -Duser.locale=en/US -DHARNESS_JAVA_AGENT=%s --tests \"pkg1.cls1\" --tests \"pkg2.cls2\"", agent),
			expectedErr:          false,
			tests:                []ti.RunnableTest{t1, t2, t1, t2},
		},
		{
			name:                 "RunSelectedTests_OneTest_UserParams_OrCondition_AgentAttached",
			args:                 "test -Duser.timezone=US/Mountain -Duser.locale=en/US || true",
			runOnlySelectedTests: true,
			ignoreInstr:          false,
			want:                 fmt.Sprintf("./gradlew test -Duser.timezone=US/Mountain -Duser.locale=en/US -DHARNESS_JAVA_AGENT=%s --tests \"pkg2.cls2\" || true", agent),
			expectedErr:          false,
			tests:                []ti.RunnableTest{t2},
		},
		{
			name:                 "RunSelectedTests_OneTest_UserParams_MultipleOrCondition_AgentAttached",
			args:                 "test -Duser.timezone=US/Mountain -Duser.locale=en/US || true || false || other",
			runOnlySelectedTests: true,
			ignoreInstr:          false,
			want:                 fmt.Sprintf("./gradlew test -Duser.timezone=US/Mountain -Duser.locale=en/US -DHARNESS_JAVA_AGENT=%s --tests \"pkg2.cls2\" || true || false || other", agent),
			expectedErr:          false,
			tests:                []ti.RunnableTest{t2},
		},
		// Ignore instrumentation true: Manual run or RunOnlySelectedTests task input is false
		{
			name:                 "RunAllTests_AgentNotAttached",
			args:                 "test",
			runOnlySelectedTests: false,
			ignoreInstr:          true,
			want:                 "./gradlew test",
			expectedErr:          false,
			tests:                []ti.RunnableTest{t1, t2},
		},
		{
			name:                 "RunSelectedTests_TwoTests_UserParams_AgentNotAttached",
			args:                 "test -Duser.timezone=US/Mountain",
			runOnlySelectedTests: true,
			ignoreInstr:          true,
			want:                 "./gradlew test -Duser.timezone=US/Mountain --tests \"pkg1.cls1\" --tests \"pkg2.cls2\"",
			expectedErr:          false,
			tests:                []ti.RunnableTest{t1, t2},
		},
		{
			name:                 "RunSelectedTests_ZeroTests_UserParams_AgentNotAttached",
			args:                 "test -Duser.timezone=US/Mountain -Duser.locale=en/US",
			runOnlySelectedTests: true,
			ignoreInstr:          true,
			want:                 "echo \"Skipping test run, received no tests to execute\"",
			expectedErr:          false,
			tests:                []ti.RunnableTest{},
		},
		{
			name:                 "RunSelectedTests_TwoTests__Duplicate_UserParams_AgentNotAttached",
			args:                 "test -Duser.timezone=US/Mountain -Duser.locale=en/US",
			runOnlySelectedTests: true,
			ignoreInstr:          true,
			want:                 "./gradlew test -Duser.timezone=US/Mountain -Duser.locale=en/US --tests \"pkg1.cls1\" --tests \"pkg2.cls2\"",
			expectedErr:          false,
			tests:                []ti.RunnableTest{t1, t2, t1, t2},
		},
		{
			name:                 "RunSelectedTests_OneTest_UserParams_OrCondition_AgentNotAttached",
			args:                 "test -Duser.timezone=US/Mountain -Duser.locale=en/US || true",
			runOnlySelectedTests: true,
			ignoreInstr:          true,
			want:                 "./gradlew test -Duser.timezone=US/Mountain -Duser.locale=en/US --tests \"pkg2.cls2\" || true",
			expectedErr:          false,
			tests:                []ti.RunnableTest{t2},
		},
		{
			name:                 "RunSelectedTests_OneTest_UserParams_MultipleOrCondition_AgentNotAttached",
			args:                 "test -Duser.timezone=US/Mountain -Duser.locale=en/US || true || false || other",
			runOnlySelectedTests: true,
			ignoreInstr:          true,
			want:                 "./gradlew test -Duser.timezone=US/Mountain -Duser.locale=en/US --tests \"pkg2.cls2\" || true || false || other",
			expectedErr:          false,
			tests:                []ti.RunnableTest{t2},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := runner.GetCmd(ctx, tc.tests, tc.args, "/path/to/workspace", "/test/tmp/config.ini", "/install/dir/java/", tc.ignoreInstr, !tc.runOnlySelectedTests, common.RunnerArgs{})
			if tc.expectedErr == (err == nil) {
				t.Fatalf("%s: expected error: %v, got: %v", tc.name, tc.expectedErr, got)
			}
			assert.Equal(t, got, tc.want, tc.name)
		})
	}
}
