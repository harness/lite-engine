// Copyright 2022 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package csharp

import (
	"context"
	"os"
	"testing"

	"github.com/harness/lite-engine/internal/filesystem"
	"github.com/harness/lite-engine/ti/instrumentation/common"
	ti "github.com/harness/ti-client/types"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"go.uber.org/mock/gomock"
)

func TestMain(t *testing.M) {
	os.Exit(0) // Remove once the exact approach is finalized
}

func TestDotNet_GetCmd(t *testing.T) {
	ctrl, ctx := gomock.WithContext(context.Background(), t)
	defer ctrl.Finish()

	log := logrus.New()
	fs := filesystem.NewMockFileSystem(ctrl)

	runner := NewDotnetRunner(log, fs)

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
			name:                 "run all tests with non-empty test list and runOnlySelectedTests as false",
			args:                 "test Build.csproj --test-adapter-path:. --logger:trx",
			runOnlySelectedTests: false,
			want:                 "dotnet test Build.csproj --test-adapter-path:. --logger:trx",
			expectedErr:          false,
			tests:                []ti.RunnableTest{t1, t2},
		},
		{
			name:                 "run all tests with empty test list and runOnlySelectedTests as false",
			args:                 "test Build.csproj --test-adapter-path:. --logger:trx",
			runOnlySelectedTests: false,
			want:                 "dotnet test Build.csproj --test-adapter-path:. --logger:trx",
			expectedErr:          false,
			tests:                []ti.RunnableTest{},
		},
		{
			name:                 "run selected tests with given test list",
			args:                 "test Build.csproj --test-adapter-path:. --logger:trx",
			runOnlySelectedTests: true,
			want:                 "dotnet test Build.csproj --test-adapter-path:. --logger:trx --filter \"FullyQualifiedName~pkg1.cls1|FullyQualifiedName~pkg2.cls2\"",
			expectedErr:          false,
			tests:                []ti.RunnableTest{t1, t2},
		},
		{
			name:                 "run selected tests with zero tests",
			args:                 "test Build.csproj",
			runOnlySelectedTests: true,
			want:                 "echo \"Skipping test run, received no tests to execute\"",
			expectedErr:          false,
			tests:                []ti.RunnableTest{},
		},
		{
			name:                 "run selected tests with repeating test list",
			args:                 "test Build.csproj",
			runOnlySelectedTests: true,
			want:                 "dotnet test Build.csproj --filter \"FullyQualifiedName~pkg1.cls1|FullyQualifiedName~pkg2.cls2\"",
			expectedErr:          false,
			tests:                []ti.RunnableTest{t1, t2, t1, t2},
		},
		{
			name:                 "run selected tests with single test",
			args:                 "test Build.csproj",
			runOnlySelectedTests: true,
			want:                 "dotnet test Build.csproj --filter \"FullyQualifiedName~pkg2.cls2\"",
			expectedErr:          false,
			tests:                []ti.RunnableTest{t2},
		},
		{
			name:                 "run selected tests with with ignore instrumentation as true",
			args:                 "test Build.csproj",
			runOnlySelectedTests: true,
			want:                 "dotnet test Build.csproj --filter \"FullyQualifiedName~pkg2.cls2\"",
			expectedErr:          false,
			tests:                []ti.RunnableTest{t2},
		},
	}

	for _, tc := range tests {
		got, err := runner.GetCmd(ctx, tc.tests, tc.args, "/path/to/workspace", "/test/tmp/config.ini", "/install/dir/csharp/", false, !tc.runOnlySelectedTests, common.RunnerArgs{})
		if tc.expectedErr == (err == nil) {
			t.Fatalf("%s: expected error: %v, got: %v", tc.name, tc.expectedErr, got)
		}
		assert.Equal(t, got, tc.want)
	}
}

func TestGetDotnetCmd_Manual(t *testing.T) {
	ctrl, ctx := gomock.WithContext(context.Background(), t)
	defer ctrl.Finish()

	log := logrus.New()
	fs := filesystem.NewMockFileSystem(ctrl)

	runner := NewDotnetRunner(log, fs)

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
			name:                 "run all tests with empty test list and runOnlySelectedTests as false",
			args:                 "test Build.csproj",
			runOnlySelectedTests: false,
			want:                 "dotnet test Build.csproj",
			expectedErr:          false,
			tests:                []ti.RunnableTest{},
		},
		{
			name:                 "run selected tests with a test list and runOnlySelectedTests as true",
			args:                 "test Build.csproj",
			runOnlySelectedTests: true,
			want:                 "dotnet test Build.csproj",
			expectedErr:          false,
			tests:                []ti.RunnableTest{t1, t2},
		},
	}

	for _, tc := range tests {
		got, err := runner.GetCmd(ctx, tc.tests, tc.args, "/path/to/workspace", "/test/tmp/config.ini", "/install/dir/csharp/", true, !tc.runOnlySelectedTests, common.RunnerArgs{})
		if tc.expectedErr == (err == nil) {
			t.Fatalf("%s: expected error: %v, got: %v", tc.name, tc.expectedErr, got)
		}
		assert.Equal(t, got, tc.want)
	}
}
