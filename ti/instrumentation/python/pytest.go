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
	pytestCmd = "pytest"
)

type pytestRunner struct {
	fs  filesystem.FileSystem
	log *logrus.Logger
}

func NewPytestRunner(log *logrus.Logger, fs filesystem.FileSystem) *pytestRunner { //nolint:revive
	log.Infoln("NewPytestRunner starting")

	return &pytestRunner{
		fs:  fs,
		log: log,
	}
}

func (m *pytestRunner) AutoDetectPackages(workspace string) ([]string, error) {
	return []string{}, errors.New("not implemented")
}

func (m *pytestRunner) AutoDetectTests(ctx context.Context, workspace string, testGlobs []string) ([]ti.RunnableTest, error) {
	tests := make([]ti.RunnableTest, 0)
	pythonTests, err := GetPythonTests(testGlobs)
	if err != nil {
		return tests, err
	}
	tests = append(tests, pythonTests...)
	return tests, nil
}

func (m *pytestRunner) GetCmd(ctx context.Context, tests []ti.RunnableTest, userArgs, workspace,
	agentConfigPath, agentInstallDir string, ignoreInstr, runAll bool) (string, error) {
	// Run all the tests
	m.log.Infoln("Pytest GetCmd starting")

	if runAll {
		if ignoreInstr {
			return strings.TrimSpace(fmt.Sprintf("%s %s", pytestCmd, userArgs)), nil
		}
		return strings.TrimSpace(
			fmt.Sprintf("python3 python_agent.py %s --test_harness %s --disable_static",
				".", pytestCmd)), nil
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
		return strings.TrimSpace(fmt.Sprintf("%s %s %s", pytestCmd, testStr, userArgs)), nil
	}

	return fmt.Sprintf("python3 python_agent.py %s --test_harness %s --disable_static",
		".", pytestCmd), nil
}
