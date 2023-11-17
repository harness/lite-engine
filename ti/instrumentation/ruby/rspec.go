// Copyright 2021 Harness Inc. All rights reserved.
// Use of this source code is governed by the PolyForm Free Trial 1.0.0 license
// that can be found in the licenses directory at the root of this repository, also available at
// https://polyformproject.org/wp-content/uploads/2020/05/PolyForm-Free-Trial-1.0.0.txt.

package ruby

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/harness/lite-engine/internal/filesystem"
	"github.com/harness/lite-engine/ti/instrumentation/common"
	ti "github.com/harness/ti-client/types"

	"github.com/sirupsen/logrus"
)

var (
	rspecCmd = "bundle exec rspec"
)

type rspecRunner struct {
	fs  filesystem.FileSystem
	log *logrus.Logger
}

func NewRubyRunner(log *logrus.Logger, fs filesystem.FileSystem) *rspecRunner { //nolint:revive
	return &rspecRunner{
		fs:  fs,
		log: log,
	}
}

func (m *rspecRunner) AutoDetectPackages(workspace string) ([]string, error) {
	return []string{}, errors.New("not implemented")
}

func (m *rspecRunner) AutoDetectTests(ctx context.Context, workspace string, testGlobs []string) ([]ti.RunnableTest, error) {
	pythonTests := GetRubyTests(workspace, testGlobs)
	return pythonTests, nil
}

func (m *rspecRunner) ReadPackages(workspace string, files []ti.File) []ti.File {
	return files
}

func (m *rspecRunner) GetCmd(ctx context.Context, tests []ti.RunnableTest, userArgs, workspace,
	agentConfigPath, agentInstallDir string, ignoreInstr, runAll bool) (string, error) {
	testCmd := ""
	tiFlag := "TI=1"
	if !ignoreInstr {
		repoPath, err := UnzipAndGetTestInfo(agentInstallDir, m.log)
		if err != nil {
			return "", err
		}
		err = WriteGemFile(workspace, repoPath)
		if err != nil {
			return testCmd, err
		}
		err = WriteHelperFile(repoPath)
		if err != nil {
			m.log.Errorln("Unable to write rspec helper file automatically", err)
		}
	}
	// Run all the tests
	if userArgs == "" {
		userArgs = fmt.Sprintf("--format RspecJunitFormatter --out %s${HARNESS_NODE_INDEX}", common.HarnessDefaultReportPath)
	}

	if runAll {
		if ignoreInstr {
			return strings.TrimSpace(fmt.Sprintf("%s %s", rspecCmd, userArgs)), nil
		}
		testCmd = strings.TrimSpace(fmt.Sprintf("%s %s %s ",
			tiFlag, rspecCmd, userArgs))
		return testCmd, nil
	}

	if len(tests) == 0 {
		return "echo \"Skipping test run, received no tests to execute\"", nil
	}

	ut := common.GetUniqueTestStrings(tests)
	testStr := strings.Join(ut, " ")

	if ignoreInstr {
		return strings.TrimSpace(fmt.Sprintf("%s %s %s", rspecCmd, userArgs, testStr)), nil
	}

	testCmd = fmt.Sprintf("%s %s %s %s",
		tiFlag, rspecCmd, userArgs, testStr)
	return testCmd, nil
}
