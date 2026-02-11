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
	"github.com/harness/lite-engine/ti/instrumentation/common"
	ti "github.com/harness/ti-client/types"
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

func (s *sbtRunner) AutoDetectTests(ctx context.Context, workspace string, testGlobs []string) ([]ti.RunnableTest, error) {
	tests := make([]ti.RunnableTest, 0)
	javaTests := GetJavaTests(workspace, testGlobs)
	scalaTests := GetScalaTests(workspace, testGlobs)

	tests = append(tests, javaTests...)
	tests = append(tests, scalaTests...)
	return tests, nil
}

func (s *sbtRunner) ReadPackages(workspace string, files []ti.File) []ti.File {
	return ReadPkgs(s.log, s.fs, workspace, files)
}

func (s *sbtRunner) GetTestGlobs() (testGlobs, excludeGlobs []string) {
	return make([]string, 0), make([]string, 0)
}

func (s *sbtRunner) GetCmd(ctx context.Context, tests []ti.RunnableTest, userArgs, workspace,
	agentConfigPath, agentInstallDir string, ignoreInstr, runAll bool, runnerArgs common.RunnerArgs) (string, error) {
	// Agent arg
	javaAgentPath := filepath.Join(agentInstallDir, JavaAgentJar)
	agentArg := fmt.Sprintf(AgentArg, javaAgentPath, agentConfigPath)
	instrArg := fmt.Sprintf("'set javaOptions ++= Seq(\"%s\")'", agentArg) //nolint:gocritic

	// Run all the tests
	if runAll {
		if ignoreInstr {
			return fmt.Sprintf("%s %s 'test'", sbtCmd, userArgs), nil
		}
		return fmt.Sprintf("%s %s %s 'test'", sbtCmd, userArgs, instrArg), nil
	}
	if len(tests) == 0 {
		return SkipTestRunMsg, nil
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
	if ignoreInstr {
		return fmt.Sprintf("%s %s 'testOnly %s'", sbtCmd, userArgs, strings.Join(testsList, " ")), nil
	}
	return fmt.Sprintf("%s %s %s 'testOnly %s'", sbtCmd, userArgs, instrArg, strings.Join(testsList, " ")), nil
}
