// Copyright 2021 Harness Inc. All rights reserved.
// Use of this source code is governed by the PolyForm Free Trial 1.0.0 license
// that can be found in the licenses directory at the root of this repository, also available at
// https://polyformproject.org/wp-content/uploads/2020/05/PolyForm-Free-Trial-1.0.0.txt.

package ruby

import (
	"context"
	"fmt"
	"strings"

	"github.com/harness/lite-engine/internal/filesystem"
	"github.com/harness/lite-engine/ti/instrumentation/common"
	ti "github.com/harness/ti-client/types"

	"github.com/sirupsen/logrus"
)

const (
	rspecCmd = "bundle exec rspec"
)

type rspecRunner struct {
	fs        filesystem.FileSystem
	log       *logrus.Logger
	testGlobs []string
	envs      map[string]string
}

func NewRubyRunner(log *logrus.Logger, fs filesystem.FileSystem, testGlobs []string, envs map[string]string) *rspecRunner { //nolint:revive
	return &rspecRunner{
		fs:        fs,
		log:       log,
		testGlobs: testGlobs,
		envs:      envs,
	}
}

func (m *rspecRunner) AutoDetectPackages(workspace string) ([]string, error) {
	return []string{}, nil
}

func (m *rspecRunner) AutoDetectTests(ctx context.Context, workspace string, testGlobs []string) ([]ti.RunnableTest, error) {
	testGlobs, excludeGlobs := GetRubyGlobs(testGlobs, m.envs)
	rubyTests, err := GetRubyTests(workspace, testGlobs, excludeGlobs, m.log)
	return rubyTests, err
}

func (m *rspecRunner) ReadPackages(workspace string, files []ti.File) []ti.File {
	return files
}

func (m *rspecRunner) GetTestGlobs() (includeGlobs, excludeGlobs []string) {
	return GetRubyGlobs(m.testGlobs, m.envs)
}

func (m *rspecRunner) GetCmd(ctx context.Context, tests []ti.RunnableTest, userArgs, workspace,
	agentConfigPath, agentInstallDir string, ignoreInstr, runAll bool, runnerArgs common.RunnerArgs) (string, error) {
	testCmd := ""
	tiFlag := "TI=1"
	installReportCmd := ""
	installAgentCmd := ""
	if !ignoreInstr {
		repoPath, err := UnzipAndGetTestInfo(agentInstallDir, m.log)
		if err != nil {
			return "", err
		}
		installAgentCmd = fmt.Sprintf("bundle add harness_ruby_agent --path %q --version %q || true;", repoPath, "0.0.1")
		err = WriteHelperFile(workspace, repoPath)
		if err != nil {
			m.log.Errorln("Unable to write rspec helper file automatically", err)
		}
	}
	// Run all the tests
	if userArgs == "" {
		installReportCmd = "bundle add rspec_junit_formatter || true;"
		userArgs = fmt.Sprintf("--format RspecJunitFormatter --out %s${HARNESS_NODE_INDEX}", common.HarnessDefaultReportPath)
	}

	if runAll {
		rspecGlob := ""
		if len(m.testGlobs) > 0 {
			rspecGlob = "--pattern " + strings.Join(m.testGlobs, " ")
		}
		if ignoreInstr {
			return strings.TrimSpace(fmt.Sprintf("%s %s %s %s", installReportCmd, rspecCmd, userArgs, rspecGlob)), nil
		}
		testCmd = strings.TrimSpace(fmt.Sprintf("%s %s %s %s %s %s",
			installReportCmd, installAgentCmd, tiFlag, rspecCmd, userArgs, rspecGlob))
		return testCmd, nil
	}

	if len(tests) == 0 {
		return "echo \"Skipping test run, received no tests to execute\"", nil
	}

	ut := common.GetUniqueTestStrings(tests)
	testStr := strings.Join(ut, " ")

	if ignoreInstr {
		return strings.TrimSpace(fmt.Sprintf("%s %s %s %s", installAgentCmd, rspecCmd, userArgs, testStr)), nil
	}

	testCmd = fmt.Sprintf("%s %s %s %s %s %s",
		installReportCmd, installAgentCmd, tiFlag, rspecCmd, userArgs, testStr)
	return testCmd, nil
}
