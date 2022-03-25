// Copyright 2022 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package java

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/harness/lite-engine/internal/filesystem"
	"github.com/harness/lite-engine/ti"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

func TestGetGradleCmd(t *testing.T) {
	ctrl, ctx := gomock.WithContext(context.Background(), t)
	defer ctrl.Finish()

	log := logrus.New()
	fs := filesystem.NewMockFileSystem(ctrl)

	fs.EXPECT().Stat("gradlew").Return(nil, nil).AnyTimes()

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
		want                 string
		expectedErr          bool
		tests                []ti.RunnableTest
	}{
		{
			name:                 "run all tests with run only selected tests as false",
			args:                 "test",
			runOnlySelectedTests: false,
			want:                 fmt.Sprintf("./gradlew test -DHARNESS_JAVA_AGENT=%s", agent),
			expectedErr:          false,
			tests:                []ti.RunnableTest{t1, t2},
		},
		{
			name:                 "run selected tests with given test list and extra args",
			args:                 "test -Duser.timezone=US/Mountain",
			runOnlySelectedTests: true,
			want:                 fmt.Sprintf("./gradlew test -Duser.timezone=US/Mountain -DHARNESS_JAVA_AGENT=%s --tests \"pkg1.cls1\" --tests \"pkg2.cls2\"", agent),
			expectedErr:          false,
			tests:                []ti.RunnableTest{t1, t2},
		},
		{
			name:                 "run selected tests with zero tests",
			args:                 "test -Duser.timezone=US/Mountain -Duser.locale=en/US",
			runOnlySelectedTests: true,
			want:                 "echo \"Skipping test run, received no tests to execute\"",
			expectedErr:          false,
			tests:                []ti.RunnableTest{},
		},
		{
			name:                 "run selected tests with repeating test list and -Duser parameters",
			args:                 "test -Duser.timezone=US/Mountain -Duser.locale=en/US",
			runOnlySelectedTests: true,
			want:                 fmt.Sprintf("./gradlew test -Duser.timezone=US/Mountain -Duser.locale=en/US -DHARNESS_JAVA_AGENT=%s --tests \"pkg1.cls1\" --tests \"pkg2.cls2\"", agent),
			expectedErr:          false,
			tests:                []ti.RunnableTest{t1, t2, t1, t2},
		},
		{
			name:                 "run selected tests with single test and -Duser parameters and or condition",
			args:                 "test -Duser.timezone=US/Mountain -Duser.locale=en/US || true",
			runOnlySelectedTests: true,
			want:                 fmt.Sprintf("./gradlew test -Duser.timezone=US/Mountain -Duser.locale=en/US -DHARNESS_JAVA_AGENT=%s --tests \"pkg2.cls2\" || true", agent),
			expectedErr:          false,
			tests:                []ti.RunnableTest{t2},
		},
		{
			name:                 "run selected tests with single test and -Duser parameters and multiple or conditions",
			args:                 "test -Duser.timezone=US/Mountain -Duser.locale=en/US || true || false || other",
			runOnlySelectedTests: true,
			want:                 fmt.Sprintf("./gradlew test -Duser.timezone=US/Mountain -Duser.locale=en/US -DHARNESS_JAVA_AGENT=%s --tests \"pkg2.cls2\" || true || false || other", agent),
			expectedErr:          false,
			tests:                []ti.RunnableTest{t2},
		},
	}

	for _, tc := range tests {
		got, err := runner.GetCmd(ctx, tc.tests, tc.args, "/path/to/workspace", "/test/tmp/config.ini", "/install/dir/java/", false, !tc.runOnlySelectedTests)
		if tc.expectedErr == (err == nil) {
			t.Fatalf("%s: expected error: %v, got: %v", tc.name, tc.expectedErr, got)
		}
		assert.Equal(t, got, tc.want, tc.name)
	}
}

func TestGetGradleCmd_Manual(t *testing.T) {
	ctrl, ctx := gomock.WithContext(context.Background(), t)
	defer ctrl.Finish()

	log := logrus.New()
	fs := filesystem.NewMockFileSystem(ctrl)

	fs.EXPECT().Stat("gradlew").Return(nil, os.ErrNotExist).AnyTimes()

	runner := NewGradleRunner(log, fs)

	tests := []struct {
		name                 string // description of test
		args                 string
		runOnlySelectedTests bool
		want                 string
		expectedErr          bool
		tests                []ti.RunnableTest
	}{
		{
			name:                 "run all tests with empty test list and run only selected tests as false",
			args:                 "test -Duser.timezone=en/US",
			runOnlySelectedTests: false,
			want:                 "gradle test -Duser.timezone=en/US",
			expectedErr:          false,
			tests:                []ti.RunnableTest{},
		},
		{
			name:                 "run selected tests with run only selected tests as true",
			args:                 "test -Duser.timezone=US/Mountain -Duser.locale=en/US",
			runOnlySelectedTests: true,
			want:                 "gradle test -Duser.timezone=US/Mountain -Duser.locale=en/US",
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
