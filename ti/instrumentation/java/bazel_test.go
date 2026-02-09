// Copyright 2022 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package java

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"testing"

	"github.com/harness/lite-engine/internal/filesystem"
	"github.com/harness/lite-engine/ti/instrumentation/common"
	ti "github.com/harness/ti-client/types"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"go.uber.org/mock/gomock"
)

const bazelRuleStringsBazelAutoDetectTests = "//module1:pkg1.cls1\n//module1:pkg1.cls2\n//module1:pkg2\n//module1:pkg2/cls2\n"
const bazelQuery = "//120-ng-manager:io.harness.ng.GenerateOpenApiSpecCommandTest\n//220-graphql-test:io.harness.GraphQLExceptionHandlingTest\n//pipeline-service/service:io.harness.GenerateOpenApiSpecCommandTest\n" //nolint:lll

func TestGetBazelCmd(_ *testing.T) {
	// Bazel impl is pretty hacky right now and tailored to running portal.
	// Will add this once we have a more generic implementation.
}

func fakeExecCommand(ctx context.Context, command string, args ...string) *exec.Cmd {
	cs := []string{"-test.run=TestHelperProcess", "--", command}
	cs = append(cs, args...)
	cmd := exec.CommandContext(ctx, os.Args[0], cs...) //nolint:gosec
	cmd.Env = []string{"GO_WANT_HELPER_PROCESS=1"}
	return cmd
}

func fakeExecCommand2(ctx context.Context, command string, args ...string) *exec.Cmd {
	cs := []string{"-test.run=TestHelperProcess2", "--", command}
	cs = append(cs, args...)
	cmd := exec.CommandContext(ctx, os.Args[0], cs...) //nolint:gosec
	cmd.Env = []string{"GO_WANT_HELPER_PROCESS=1"}
	return cmd
}

func fakeExecCommand3(ctx context.Context, command string, args ...string) *exec.Cmd {
	cs := []string{"-test.run=TestHelperProcess3", "--", command}
	cs = append(cs, args...)
	cmd := exec.CommandContext(ctx, os.Args[0], cs...) //nolint:gosec
	cmd.Env = []string{"GO_WANT_HELPER_PROCESS=1"}
	return cmd
}

func TestHelperProcess(_ *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}
	fmt.Fprintf(os.Stdout, bazelRuleStringsBazelAutoDetectTests) //nolint:staticcheck
	os.Exit(0)
}

func TestHelperProcess2(_ *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}
	fmt.Fprintf(os.Stdout, bazelQuery) //nolint:staticcheck
	os.Exit(0)
}

func TestHelperProcess3(_ *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}
	fmt.Fprintf(os.Stdout, "//module1:io.harness.SomeTest")
	os.Exit(0)
}

func TestBazelAutoDetectTests(t *testing.T) {
	ctrl, ctx := gomock.WithContext(context.Background(), t)
	defer ctrl.Finish()

	log := logrus.New()
	fs := filesystem.NewMockFileSystem(ctrl)

	runner := NewBazelRunner(log, fs)

	execCmdCtx = fakeExecCommand
	defer func() {
		execCmdCtx = exec.CommandContext
	}()

	t1 := ti.RunnableTest{Pkg: "pkg1", Class: "cls1"}
	t1.Autodetect.Rule = "//module1:pkg1.cls1"
	t2 := ti.RunnableTest{Pkg: "pkg1", Class: "cls2"}
	t2.Autodetect.Rule = "//module1:pkg1.cls2"
	// t3 is invalid
	t4 := ti.RunnableTest{Pkg: "pkg2", Class: "cls2"}
	t4.Autodetect.Rule = "//module1:pkg2/cls2"

	// The tests are repeated because the mock exec command is hardcoded to return t1, t2, t4 for
	// any bazel query irrespective of java/scala/kt.
	testsExpected := []ti.RunnableTest{t1, t2, t4, t1, t2, t4, t1, t2, t4}

	tests, _ := runner.AutoDetectTests(ctx, "", []string{})
	assert.Equal(t, testsExpected, tests)
}

func TestGetBazelCmd_TestsWithRules(t *testing.T) {
	ctrl, ctx := gomock.WithContext(context.Background(), t)
	defer ctrl.Finish()

	log := logrus.New()
	fs := filesystem.NewMockFileSystem(ctrl)

	runner := NewBazelRunner(log, fs)

	t1 := ti.RunnableTest{Pkg: "pkg1", Class: "cls1"}
	t1.Autodetect.Rule = "//module1:pkg1.cls1"
	t2 := ti.RunnableTest{Pkg: "pkg1", Class: "cls2"}
	t2.Autodetect.Rule = "//module1:pkg1.cls2"
	t3 := ti.RunnableTest{Pkg: "pkg2", Class: "cls2"}
	t3.Autodetect.Rule = "//module1:pkg2/cls2"
	tests := []ti.RunnableTest{t1, t2, t3}
	expectedCmd := "bazel  --define=HARNESS_ARGS=-javaagent:java-agent.jar=/test/tmp/config.ini //module1:pkg1.cls1 //module1:pkg1.cls2 //module1:pkg2/cls2"

	cmd, _ := runner.GetCmd(ctx, tests, "", "", "/test/tmp/config.ini", "", false, false, common.RunnerArgs{})
	assert.Equal(t, expectedCmd, cmd)
}

func TestGetBazelCmd_TestsWithModuleRules(t *testing.T) {
	ctrl, ctx := gomock.WithContext(context.Background(), t)
	defer ctrl.Finish()

	log := logrus.New()
	fs := filesystem.NewMockFileSystem(ctrl)

	runner := NewBazelRunner(log, fs)
	runnerArg := common.RunnerArgs{}
	runnerArg.ModuleList = []string{"module1", "module3"}
	execCmdCtx = fakeExecCommand3
	defer func() {
		execCmdCtx = exec.CommandContext
	}()

	t1 := ti.RunnableTest{Pkg: "pkg2", Class: "cls1"}
	t1.Autodetect.Rule = "//module1:pkg2.cls1"
	t2 := ti.RunnableTest{Pkg: "pkg3", Class: "cls2"}
	t2.Autodetect.Rule = "//module2:pkg3.cls2"
	t3 := ti.RunnableTest{Pkg: "pkg2", Class: "cls3"}
	t3.Autodetect.Rule = "//module2:pkg2/cls3"
	t4 := ti.RunnableTest{Pkg: "pkg3", Class: "cls4"}
	t4.Autodetect.Rule = "//module1:pkg3/cls4"
	tests := []ti.RunnableTest{t1, t2, t3, t4}
	expectedCmd := "bazel  --define=HARNESS_ARGS=-javaagent:java-agent.jar=/test/tmp/config.ini //module1/... //module3/... //module2:pkg3.cls2 //module2:pkg2/cls3"

	cmd, _ := runner.GetCmd(ctx, tests, "", "", "/test/tmp/config.ini", "", false, false, runnerArg)
	assert.Equal(t, expectedCmd, cmd)
}

func TestGetBazelCmd_GetBazelTests(t *testing.T) {
	ctrl, ctx := gomock.WithContext(context.Background(), t)
	defer ctrl.Finish()

	log := logrus.New()
	execCmdCtx = fakeExecCommand2
	defer func() {
		execCmdCtx = exec.CommandContext
	}()

	tests := []ti.RunnableTest{
		{
			Pkg:   "io.harness",
			Class: "GraphQLExceptionHandlingTest",
		},
		{
			Pkg:   "io.harness",
			Class: "GenerateOpenApiSpecCommandTest",
		},
		{
			Pkg:   "io.harness.ng",
			Class: "GenerateOpenApiSpecCommandTest",
		},
		{
			Pkg:   "io.harness.mongo",
			Class: "MongoIndexesTest",
		},
	}
	expectedTests := []ti.RunnableTest{
		{
			Pkg:   "io.harness",
			Class: "GraphQLExceptionHandlingTest",
		},
		{
			Pkg:   "io.harness",
			Class: "GenerateOpenApiSpecCommandTest",
		},
		{
			Pkg:   "io.harness.ng",
			Class: "GenerateOpenApiSpecCommandTest",
		},
		{
			Pkg:   "io.harness.mongo",
			Class: "MongoIndexesTest",
		},
	}
	expectedTests[0].Autodetect.Rule = "//220-graphql-test:io.harness.GraphQLExceptionHandlingTest"
	expectedTests[1].Autodetect.Rule = "//pipeline-service/service:io.harness.GenerateOpenApiSpecCommandTest"
	expectedTests[2].Autodetect.Rule = "//120-ng-manager:io.harness.ng.GenerateOpenApiSpecCommandTest"

	tests = getBazelTestRules(ctx, log, tests, "/harness")
	fmt.Println(tests)
	assert.Equal(t, expectedTests, tests)
}
