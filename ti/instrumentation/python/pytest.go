// Copyright 2021 Harness Inc. All rights reserved.
// Use of this source code is governed by the PolyForm Free Trial 1.0.0 license
// that can be found in the licenses directory at the root of this repository, also available at
// https://polyformproject.org/wp-content/uploads/2020/05/PolyForm-Free-Trial-1.0.0.txt.

package python

import (
	"context"
	"fmt"
	"strings"

	"github.com/harness/lite-engine/internal/filesystem"
	"github.com/harness/lite-engine/ti/instrumentation/common"
	ti "github.com/harness/ti-client/types"

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
	return []string{}, nil
}

func (m *pytestRunner) AutoDetectTests(ctx context.Context, workspace string, testGlobs []string) ([]ti.RunnableTest, error) {
	pythonTests := GetPythonTests(workspace, testGlobs)
	return pythonTests, nil
}

func (m *pytestRunner) ReadPackages(workspace string, files []ti.File) []ti.File {
	return files
}

func (m *pytestRunner) GetTestGlobs() (includeGlobs, excludeGlobs []string) {
	return GetPythonGlobs(m.testGlobs)
}

func (m *pytestRunner) GetCmd(ctx context.Context, tests []ti.RunnableTest, userArgs, workspace,
	agentConfigPath, agentInstallDir string, ignoreInstr, runAll bool, runnerArgs common.RunnerArgs) (string, error) {
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

	ut := common.GetUniqueTestStrings(tests)

	if ignoreInstr {
		testStr := strings.Join(ut, " ")
		return strings.TrimSpace(fmt.Sprintf("%s -m %s %s %s", pythonCmd, pytestCmd, testStr, userArgs)), nil
	}

	testStr := strings.Join(ut, ",")
	testCmd = fmt.Sprintf("%s %s %s --test_harness %q --test_files %s",
		pythonCmd, scriptPath, currentDir, testHarness, testStr)
	return testCmd, nil
}
