// Copyright 2022 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package java

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/harness/lite-engine/internal/filesystem"
	"github.com/harness/lite-engine/ti"
	"github.com/sirupsen/logrus"
)

var (
	sbtCmd = "sbt"
)

type sbtRunner struct {
	fs  filesystem.FileSystem
	log *logrus.Logger
}

func NewSBTRunner(log *logrus.Logger, fs filesystem.FileSystem) *sbtRunner { //nolint:revive
	return &sbtRunner{
		fs:  fs,
		log: log,
	}
}

func (s *sbtRunner) AutoDetectPackages(workspace string) ([]string, error) {
	return DetectPkgs(workspace, s.log, s.fs)
}

func (s *sbtRunner) AutoDetectTests(ctx context.Context, workspace string) ([]ti.RunnableTest, error) {
	tests := make([]ti.RunnableTest, 0)
	javaTests := GetJavaTests(workspace)
	scalaTests := GetScalaTests(workspace)

	tests = append(tests, javaTests...)
	tests = append(tests, scalaTests...)
	return tests, nil
}

func (s *sbtRunner) GetCmd(ctx context.Context, tests []ti.RunnableTest, userArgs, workspace,
	agentConfigPath, agentInstallDir string, ignoreInstr, runAll bool) (string, error) {
	if ignoreInstr {
		return fmt.Sprintf("%s %s 'test'", sbtCmd, userArgs), nil
	}
	javaAgentPath := filepath.Join(agentInstallDir, JavaAgentJar)
	agentArg := fmt.Sprintf(AgentArg, javaAgentPath, agentConfigPath)
	instrArg := fmt.Sprintf("'set javaOptions ++= Seq(\"%s\")'", agentArg)   //nolint:gocritic
	defaultCmd := fmt.Sprintf("%s %s %s 'test'", sbtCmd, userArgs, instrArg) // run all the tests

	if runAll {
		// Run all the tests
		return defaultCmd, nil
	}
	if len(tests) == 0 {
		return "echo \"Skipping test run, received no tests to execute\"", nil
	}
	// Use only unique classes
	testsList := []string{}
	set := make(map[string]interface{})
	for _, t := range tests {
		if _, ok := set[t.Class]; ok {
			// The class has already been added
			continue
		}
		set[t.Class] = struct{}{}
		testsList = append(testsList, t.Pkg+"."+t.Class)
	}
	return fmt.Sprintf("%s %s %s 'testOnly %s'", sbtCmd, userArgs, instrArg, strings.Join(testsList, " ")), nil
}