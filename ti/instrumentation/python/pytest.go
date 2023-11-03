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
	"github.com/harness/lite-engine/ti/instrumentation/common"
	ti "github.com/harness/ti-client/types"
	"github.com/mattn/go-zglob"

	"github.com/sirupsen/logrus"
)

var (
	// we might need to take this from user in the future
	pythonCmd  = "python3"
	pytestCmd  = "pytest"
	currentDir = "."
)

type pytestRunner struct {
	fs        filesystem.FileSystem
	log       *logrus.Logger
	testGlobs []string
}

func NewPytestRunner(log *logrus.Logger, fs filesystem.FileSystem, testGlobs []string) *pytestRunner { //nolint:revive
	return &pytestRunner{
		fs:        fs,
		log:       log,
		testGlobs: testGlobs,
	}
}

func (m *pytestRunner) AutoDetectPackages(workspace string) ([]string, error) {
	return []string{}, errors.New("not implemented")
}

func (m *pytestRunner) AutoDetectTests(ctx context.Context, workspace string, testGlobs []string) ([]ti.RunnableTest, error) {
	pythonTests := GetPythonTests(workspace, testGlobs)
	return pythonTests, nil
}

func (m *pytestRunner) ReadPackages(workspace string, files []ti.File) []ti.File {
	return files
}

func (m *pytestRunner) GetCmd(ctx context.Context, tests []ti.RunnableTest, userArgs, workspace,
	agentConfigPath, agentInstallDir string, ignoreInstr, runAll bool) (string, error) {
	if userArgs == "" {
		userArgs = fmt.Sprintf("--junitxml='%s${HARNESS_NODE_INDEX}' -o junit_family='xunit1'", common.HarnessDefaultReportPath)
	}

	scriptPath, testHarness, err := UnzipAndGetTestInfo(agentInstallDir, ignoreInstr, pytestCmd, userArgs, m.log)
	if err != nil {
		return "", err
	}

	testCmd := ""
	if runAll {
		if ignoreInstr {
			return strings.TrimSpace(fmt.Sprintf("%s -m %s %s", pythonCmd, pytestCmd, userArgs)), nil
		}
		testCmd = strings.TrimSpace(fmt.Sprintf("%s %s %s --test_harness %q",
			pythonCmd, scriptPath, currentDir, testHarness))
		return testCmd, nil
	}
	if len(tests) == 0 {
		return "echo \"Skipping test run, received no tests to execute\"", nil
	}
	// Use only unique class
	set := make(map[ti.RunnableTest]interface{})
	ut := []string{}
	for _, t := range tests {
		// Only add tests matching test globs
		testGlobs := m.testGlobs
		// Don't filter if glob not specified
		if len(m.testGlobs) == 0 {
			testGlobs = []string{"**"}
		}
		for _, glob := range testGlobs {
			if matched, _ := zglob.Match(glob, t.Class); !matched {
				continue
			}
			w := ti.RunnableTest{Class: t.Class}
			if _, ok := set[w]; ok {
				// The test has already been added
				continue
			}
			set[w] = struct{}{}
			ut = append(ut, t.Class)
			break
		}
	}

	if ignoreInstr {
		testStr := strings.Join(ut, " ")
		return strings.TrimSpace(fmt.Sprintf("%s -m %s %s %s", pythonCmd, pytestCmd, testStr, userArgs)), nil
	}

	testStr := strings.Join(ut, ",")
	testCmd = fmt.Sprintf("%s %s %s --test_harness %q --test_files %s",
		pythonCmd, scriptPath, currentDir, testHarness, testStr)
	return testCmd, nil
}
