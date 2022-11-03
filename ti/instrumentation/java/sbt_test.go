// Copyright 2022 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package java

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/golang/mock/gomock"
	"github.com/harness/lite-engine/internal/filesystem"
	"github.com/harness/lite-engine/ti"
	"github.com/sirupsen/logrus"
)

func TestSBT_GetCmd(t *testing.T) {
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
		want                 string
		expectedErr          bool
		tests                []ti.RunnableTest
	}{
		{
			name:                 "run all tests with non-empty test list and -Duser parameters",
			args:                 "-Duser.timezone=US/Mountain -Duser.locale=en/US",
			runOnlySelectedTests: false,
			want:                 fmt.Sprintf("sbt -Duser.timezone=US/Mountain -Duser.locale=en/US '%s' 'test'", javaOpts),
			expectedErr:          false,
			tests:                []ti.RunnableTest{t1, t2},
		},
		{
			name:                 "run all tests with empty test list and no -Duser parameters",
			args:                 "clean test",
			runOnlySelectedTests: false,
			want:                 fmt.Sprintf("sbt clean test '%s' 'test'", javaOpts),
			expectedErr:          false,
			tests:                []ti.RunnableTest{},
		},
		{
			name:                 "run selected tests with given test list and -Duser parameters",
			args:                 "clean test -Duser.timezone=US/Mountain -Duser.locale=en/US",
			runOnlySelectedTests: true,
			want:                 fmt.Sprintf("sbt clean test -Duser.timezone=US/Mountain -Duser.locale=en/US '%s' 'testOnly pkg1.cls1 pkg2.cls2'", javaOpts),
			expectedErr:          false,
			tests:                []ti.RunnableTest{t1, t2},
		},
		{
			name:                 "run selected tests with zero tests and -Duser parameters",
			args:                 "clean test -Duser.timezone=US/Mountain -Duser.locale=en/US",
			runOnlySelectedTests: true,
			want:                 "echo \"Skipping test run, received no tests to execute\"",
			expectedErr:          false,
			tests:                []ti.RunnableTest{},
		},
		{
			name:                 "run selected tests with repeating test list and -Duser parameters",
			args:                 "clean test -B -2C-Duser.timezone=US/Mountain -Duser.locale=en/US",
			runOnlySelectedTests: true,
			want:                 fmt.Sprintf("sbt clean test -B -2C-Duser.timezone=US/Mountain -Duser.locale=en/US '%s' 'testOnly pkg1.cls1 pkg2.cls2'", javaOpts),
			expectedErr:          false,
			tests:                []ti.RunnableTest{t1, t2, t1, t2},
		},
		{
			name:                 "run selected tests with single test and -Duser parameters and or condition",
			args:                 "clean test -B -2C -Duser.timezone=US/Mountain -Duser.locale=en/US || true",
			runOnlySelectedTests: true,
			want:                 fmt.Sprintf("sbt clean test -B -2C -Duser.timezone=US/Mountain -Duser.locale=en/US || true '%s' 'testOnly pkg2.cls2'", javaOpts),
			expectedErr:          false,
			tests:                []ti.RunnableTest{t2},
		},
	}

	for _, tc := range tests {
		got, err := runner.GetCmd(ctx, tc.tests, tc.args, "/path/to/workspace", "/test/tmp/config.ini", "/install/dir/java/", false, !tc.runOnlySelectedTests)
		if tc.expectedErr == (err == nil) {
			t.Fatalf("%s: expected error: %v, got: %v", tc.name, tc.expectedErr, got)
		}
		assert.Equal(t, got, tc.want)
	}
}

func TestGetSBTCmd_Manual(t *testing.T) {
	ctrl, ctx := gomock.WithContext(context.Background(), t)
	defer ctrl.Finish()

	log := logrus.New()
	fs := filesystem.NewMockFileSystem(ctrl)

	runner := NewSBTRunner(log, fs)

	tests := []struct {
		name                 string // description of test
		args                 string
		runOnlySelectedTests bool
		want                 string
		expectedErr          bool
		tests                []ti.RunnableTest
	}{
		{
			name:                 "run all tests with empty test list and no -Duser parameters",
			args:                 "clean",
			runOnlySelectedTests: false,
			want:                 "sbt clean 'test'",
			expectedErr:          false,
			tests:                []ti.RunnableTest{},
		},
		{
			name:                 "run selected tests with zero tests and -Duser parameters",
			args:                 "clean test -Duser.timezone=US/Mountain -Duser.locale=en/US",
			runOnlySelectedTests: true,
			want:                 "sbt clean test -Duser.timezone=US/Mountain -Duser.locale=en/US 'test'",
			expectedErr:          false,
			tests:                []ti.RunnableTest{},
		},
	}

	for _, tc := range tests {
		got, err := runner.GetCmd(ctx, tc.tests, tc.args, "/path/to/workspace", "/test/tmp/config.ini", "/install/dir/java/", true, !tc.runOnlySelectedTests)
		if tc.expectedErr == (err == nil) {
			t.Fatalf("%s: expected error: %v, got: %v", tc.name, tc.expectedErr, got)
		}
		assert.Equal(t, got, tc.want)
	}
}
