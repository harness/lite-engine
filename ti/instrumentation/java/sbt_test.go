// Copyright 2022 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package java

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/harness/lite-engine/internal/filesystem"
	"github.com/harness/lite-engine/ti/instrumentation/common"
	ti "github.com/harness/ti-client/types"
	"github.com/sirupsen/logrus"
	"go.uber.org/mock/gomock"
)

func TestSBT_GetCmd(t *testing.T) { //nolint:funlen
	ctrl, ctx := gomock.WithContext(context.Background(), t)
	defer ctrl.Finish()

	log := logrus.New()
	fs := filesystem.NewMockFileSystem(ctrl)

	runner := NewSBTRunner(log, fs)

	t1 := ti.RunnableTest{Pkg: "pkg1", Class: "cls1", Method: "m1"}
	t2 := ti.RunnableTest{Pkg: "pkg2", Class: "cls2", Method: "m2"}
	javaOpts := "set javaOptions ++= Seq(\"-javaagent:/install/dir/java/java-agent.jar=/test/tmp/config.ini\")"

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
			name:                 "RunAllTests_UserParams_AgentAttached",
			args:                 "-Duser.timezone=US/Mountain -Duser.locale=en/US",
			runOnlySelectedTests: false,
			ignoreInstr:          false,
			want:                 fmt.Sprintf("sbt -Duser.timezone=US/Mountain -Duser.locale=en/US '%s' 'test'", javaOpts),
			expectedErr:          false,
			tests:                []ti.RunnableTest{t1, t2},
		},
		{
			name:                 "RunAllTests_AgentAttached",
			args:                 "clean test",
			runOnlySelectedTests: false,
			ignoreInstr:          false,
			want:                 fmt.Sprintf("sbt clean test '%s' 'test'", javaOpts),
			expectedErr:          false,
			tests:                []ti.RunnableTest{},
		},
		{
			name:                 "RunSelectedTests_TwoTests_UserParams_AgentAttached",
			args:                 "clean test -Duser.timezone=US/Mountain -Duser.locale=en/US",
			runOnlySelectedTests: true,
			ignoreInstr:          false,
			want:                 fmt.Sprintf("sbt clean test -Duser.timezone=US/Mountain -Duser.locale=en/US '%s' 'testOnly pkg1.cls1 pkg2.cls2'", javaOpts),
			expectedErr:          false,
			tests:                []ti.RunnableTest{t1, t2},
		},
		{
			name:                 "RunSelectedTests_ZeroTests_UserParams_AgentAttached",
			args:                 "clean test -Duser.timezone=US/Mountain -Duser.locale=en/US",
			runOnlySelectedTests: true,
			ignoreInstr:          false,
			want:                 "echo \"Skipping test run, received no tests to execute\"",
			expectedErr:          false,
			tests:                []ti.RunnableTest{},
		},
		{
			name:                 "RunSelectedTests_TwoTests_Duplicate_UserParams_AgentAttached",
			args:                 "clean test -B -2C-Duser.timezone=US/Mountain -Duser.locale=en/US",
			runOnlySelectedTests: true,
			ignoreInstr:          false,
			want:                 fmt.Sprintf("sbt clean test -B -2C-Duser.timezone=US/Mountain -Duser.locale=en/US '%s' 'testOnly pkg1.cls1 pkg2.cls2'", javaOpts),
			expectedErr:          false,
			tests:                []ti.RunnableTest{t1, t2, t1, t2},
		},
		{
			name:                 "RunSelectedTests_OneTests_UserParams_OrCondition_AgentAttached",
			args:                 "clean test -B -2C -Duser.timezone=US/Mountain -Duser.locale=en/US || true",
			runOnlySelectedTests: true,
			ignoreInstr:          false,
			want:                 fmt.Sprintf("sbt clean test -B -2C -Duser.timezone=US/Mountain -Duser.locale=en/US || true '%s' 'testOnly pkg2.cls2'", javaOpts),
			expectedErr:          false,
			tests:                []ti.RunnableTest{t2},
		},
		// Ignore instrumentation true: Manual run or RunOnlySelectedTests task input is false
		{
			name:                 "RunAllTests_UserParams_AgentNotAttached",
			args:                 "-Duser.timezone=US/Mountain -Duser.locale=en/US",
			runOnlySelectedTests: false,
			ignoreInstr:          true,
			want:                 "sbt -Duser.timezone=US/Mountain -Duser.locale=en/US 'test'",
			expectedErr:          false,
			tests:                []ti.RunnableTest{t1, t2},
		},
		{
			name:                 "RunAllTests_AgentNotAttached",
			args:                 "clean test",
			runOnlySelectedTests: false,
			ignoreInstr:          true,
			want:                 "sbt clean test 'test'",
			expectedErr:          false,
			tests:                []ti.RunnableTest{},
		},
		{
			name:                 "RunSelectedTests_TwoTests_UserParams_AgentNotAttached",
			args:                 "clean test -Duser.timezone=US/Mountain -Duser.locale=en/US",
			runOnlySelectedTests: true,
			ignoreInstr:          true,
			want:                 "sbt clean test -Duser.timezone=US/Mountain -Duser.locale=en/US 'testOnly pkg1.cls1 pkg2.cls2'",
			expectedErr:          false,
			tests:                []ti.RunnableTest{t1, t2},
		},
		{
			name:                 "RunSelectedTests_ZeroTests_UserParams_AgentNotAttached",
			args:                 "clean test -Duser.timezone=US/Mountain -Duser.locale=en/US",
			runOnlySelectedTests: true,
			ignoreInstr:          true,
			want:                 "echo \"Skipping test run, received no tests to execute\"",
			expectedErr:          false,
			tests:                []ti.RunnableTest{},
		},
		{
			name:                 "RunSelectedTests_TwoTests_Duplicate_UserParams_AgentNotAttached",
			args:                 "clean test -B -2C-Duser.timezone=US/Mountain -Duser.locale=en/US",
			runOnlySelectedTests: true,
			ignoreInstr:          true,
			want:                 "sbt clean test -B -2C-Duser.timezone=US/Mountain -Duser.locale=en/US 'testOnly pkg1.cls1 pkg2.cls2'",
			expectedErr:          false,
			tests:                []ti.RunnableTest{t1, t2, t1, t2},
		},
		{
			name:                 "RunSelectedTests_OneTests_UserParams_OrCondition_AgentNotAttached",
			args:                 "clean test -B -2C -Duser.timezone=US/Mountain -Duser.locale=en/US || true",
			runOnlySelectedTests: true,
			ignoreInstr:          true,
			want:                 "sbt clean test -B -2C -Duser.timezone=US/Mountain -Duser.locale=en/US || true 'testOnly pkg2.cls2'",
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
			assert.Equal(t, got, tc.want)
		})
	}
}
