// Copyright 2021 Harness Inc. All rights reserved.
// Use of this source code is governed by the PolyForm Free Trial 1.0.0 license
// that can be found in the licenses directory at the root of this repository, also available at
// https://polyformproject.org/wp-content/uploads/2020/05/PolyForm-Free-Trial-1.0.0.txt.

package python

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/harness/lite-engine/internal/filesystem"
	"github.com/harness/lite-engine/ti"
	"github.com/sirupsen/logrus"
)

var (
	unitTestCmd = "unittest"
)

type unittestRunner struct {
	fs  filesystem.FileSystem
	log *logrus.Logger
}

func NewUnittestRunner(log *logrus.Logger, fs filesystem.FileSystem) *unittestRunner { //nolint:revive
	log.Infoln("NewUnittestRunner starting")

	return &unittestRunner{
		fs:  fs,
		log: log,
	}
}

func (m *unittestRunner) AutoDetectPackages(workspace string) ([]string, error) {
	return []string{}, errors.New("not implemented")
}

func (m *unittestRunner) AutoDetectTests(ctx context.Context, workspace string, testGlobs []string) ([]ti.RunnableTest, error) {
	pythonTests := GetPythonTests(workspace, testGlobs)
	return pythonTests, nil
}

func (m *unittestRunner) GetCmd(ctx context.Context, tests []ti.RunnableTest, userArgs, workspace,
	agentConfigPath, agentInstallDir string, ignoreInstr, runAll bool) (string, error) {
	// Run all the tests
	m.log.Infoln("Unittest GetCmd starting")

	if runAll {
		if ignoreInstr {
			return strings.TrimSpace(fmt.Sprintf("%s %s", unitTestCmd, userArgs)), nil
		}
		return strings.TrimSpace(
			fmt.Sprintf("python3 python_agent.py %s --test_harness %s",
				".", unitTestCmd)), nil
	}
	if len(tests) == 0 {
		return fmt.Sprintf("echo \"Skipping test run, received no tests to execute\""), nil
	}
	// Use only unique <package, class> tuples
	set := make(map[ti.RunnableTest]interface{})
	ut := []string{}
	for _, t := range tests {
		w := ti.RunnableTest{Class: t.Class}
		if _, ok := set[w]; ok {
			// The test has already been added
			continue
		}
		set[w] = struct{}{}
		ut = append(ut, t.Class)
	}
	testStr := strings.Join(ut, ",")

	if ignoreInstr {
		return strings.TrimSpace(fmt.Sprintf("%s %s %s", unitTestCmd, testStr, userArgs)), nil
	}

	return fmt.Sprintf("python3 python_agent.py %s --test_harness %s --test_files %s",
		".", unitTestCmd, currentDir), nil
}
