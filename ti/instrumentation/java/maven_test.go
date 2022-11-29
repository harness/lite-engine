// Copyright 2022 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package java

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/golang/mock/gomock"
	"github.com/harness/lite-engine/internal/filesystem"
	"github.com/harness/lite-engine/ti"
	"github.com/sirupsen/logrus"
)

func TestMaven_GetCmd(t *testing.T) {
	ctrl, ctx := gomock.WithContext(context.Background(), t)
	defer ctrl.Finish()

	log := logrus.New()
	fs := filesystem.NewMockFileSystem(ctrl)

	runner := NewMavenRunner(log, fs)

	installDir := "/install/dir/java/"
	jarPath := filepath.Join(installDir, JavaAgentJar)
	agent := fmt.Sprintf(AgentArg, jarPath, "/test/tmp/config.ini")

	t1 := ti.RunnableTest{Pkg: "pkg1", Class: "cls1", Method: "m1"}
	t2 := ti.RunnableTest{Pkg: "pkg2", Class: "cls2", Method: "m2"}
	tz := "-Duser.timezone=US/Mountain"
	enUS := "-Duser.locale=en/US"

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
			args:                 "clean test -Duser.timezone=US/Mountain -Duser.locale=en/US",
			runOnlySelectedTests: false,
			want:                 fmt.Sprintf("mvn -am -DharnessArgLine=\"%s %s %s\" -DargLine=\"%s %s %s\" clean test", tz, enUS, agent, tz, enUS, agent),
			expectedErr:          false,
			tests:                []ti.RunnableTest{t1, t2},
		},
		{
			name:                 "run all tests with empty test list and no -Duser parameters",
			args:                 "clean test",
			runOnlySelectedTests: false,
			want:                 fmt.Sprintf("mvn -am -DharnessArgLine=%s -DargLine=%s clean test", agent, agent),
			expectedErr:          false,
			tests:                []ti.RunnableTest{},
		},
		{
			name:                 "run selected tests with given test list and -Duser parameters",
			args:                 "clean test -Duser.timezone=US/Mountain -Duser.locale=en/US",
			runOnlySelectedTests: true,
			want:                 fmt.Sprintf("mvn -Dtest=pkg1.cls1,pkg2.cls2 -am -DharnessArgLine=\"%s %s %s\" -DargLine=\"%s %s %s\" clean test", tz, enUS, agent, tz, enUS, agent),
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
			want:                 fmt.Sprintf("mvn -Dtest=pkg1.cls1,pkg2.cls2 -am -DharnessArgLine=\"%s %s %s\" -DargLine=\"%s %s %s\" clean test -B -2C", tz, enUS, agent, tz, enUS, agent),
			expectedErr:          false,
			tests:                []ti.RunnableTest{t1, t2, t1, t2},
		},
		{
			name:                 "run selected tests with single test and -Duser parameters and or condition",
			args:                 "clean test -B -2C -Duser.timezone=US/Mountain -Duser.locale=en/US || true",
			runOnlySelectedTests: true,
			want:                 fmt.Sprintf("mvn -Dtest=pkg2.cls2 -am -DharnessArgLine=\"%s %s %s\" -DargLine=\"%s %s %s\" clean test -B -2C   || true", tz, enUS, agent, tz, enUS, agent),
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

func TestGetMavenCmd_Manual(t *testing.T) {
	ctrl, ctx := gomock.WithContext(context.Background(), t)
	defer ctrl.Finish()

	log := logrus.New()
	fs := filesystem.NewMockFileSystem(ctrl)

	runner := NewMavenRunner(log, fs)

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
			args:                 "clean test",
			runOnlySelectedTests: false,
			want:                 "mvn clean test",
			expectedErr:          false,
			tests:                []ti.RunnableTest{},
		},
		{
			name:                 "run selected tests with zero tests",
			args:                 "",
			runOnlySelectedTests: true,
			want:                 "echo \"Skipping test run, received no tests to execute\"",
			expectedErr:          false,
			tests:                []ti.RunnableTest{},
		},
		{
			name:                 "run selected tests with with ignore instrumentation as true",
			args:                 "clean test",
			runOnlySelectedTests: true,
			want:                 "mvn -Dtest=pkg.cls clean test",
			expectedErr:          false,
			tests:                []ti.RunnableTest{{Pkg: "pkg", Class: "cls"}},
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
