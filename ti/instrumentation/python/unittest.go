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
	"github.com/mattn/go-zglob"

	"github.com/sirupsen/logrus"
)

var (
	unitTestCmd       = "unittest"
	unittestPythonCmd = "python3 -m unittest"
)

type unittestRunner struct {
	fs        filesystem.FileSystem
	log       *logrus.Logger
	testGlobs []string
}

func NewUnittestRunner(log *logrus.Logger, fs filesystem.FileSystem, testGlobs []string) *unittestRunner { //nolint:revive
	return &unittestRunner{
		fs:        fs,
		log:       log,
		testGlobs: testGlobs,
	}
}

func (m *unittestRunner) AutoDetectPackages(workspace string) ([]string, error) {
	return []string{}, nil
}

func (m *unittestRunner) AutoDetectTests(ctx context.Context, workspace string, testGlobs []string) ([]ti.RunnableTest, error) {
	pythonTests := GetPythonTests(workspace, testGlobs)
	return pythonTests, nil
}

func (m *unittestRunner) ReadPackages(workspace string, files []ti.File) []ti.File {
	return files
}

func (m *unittestRunner) GetTestGlobs() (testGlobs, excludeGlobs []string) {
	return GetPythonGlobs(m.testGlobs)
}

func (m *unittestRunner) GetCmd(ctx context.Context, tests []ti.RunnableTest, userArgs, workspace,
	agentConfigPath, agentInstallDir string, ignoreInstr, runAll bool, runnerArgs common.RunnerArgs) (string, error) {
	// Run all the tests
	scriptPath, testHarness, err := UnzipAndGetTestInfo(agentInstallDir, ignoreInstr, unitTestCmd, userArgs, m.log)
	if err != nil {
		return "", err
	}

	testCmd := ""
	if runAll {
		if ignoreInstr {
			return strings.TrimSpace(fmt.Sprintf("%s %s", unittestPythonCmd, userArgs)), nil
		}
		testCmd = strings.TrimSpace(fmt.Sprintf("python3 %s %s --test_harness %q",
			scriptPath, currentDir, testHarness))
		return testCmd, nil
	}

	if len(tests) == 0 {
		return "echo \"Skipping test run, received no tests to execute\"", nil
	}

	// Use only unique <package, class> tuples
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
	testStr := strings.Join(ut, ",")

	if ignoreInstr {
		return strings.TrimSpace(fmt.Sprintf("%s %s %s", unittestPythonCmd, testStr, userArgs)), nil
	}

	testCmd = fmt.Sprintf("python3 %s %s --test_harness %q --test_files %s",
		scriptPath, currentDir, testHarness, testStr)
	return testCmd, nil
}
