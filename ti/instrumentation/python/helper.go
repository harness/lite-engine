// Copyright 2021 Harness Inc. All rights reserved.
// Use of this source code is governed by the PolyForm Free Trial 1.0.0 license
// that can be found in the licenses directory at the root of this repository, also available at
// https://polyformproject.org/wp-content/uploads/2020/05/PolyForm-Free-Trial-1.0.0.txt.

package python

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/harness/harness-core/commons/go/lib/utils"
	"github.com/harness/lite-engine/ti"

	"github.com/mholt/archiver/v3"
	"github.com/sirupsen/logrus"
)

var (
	getFiles = utils.GetFiles
)

func getPythonTestsFromPattern(tests []ti.RunnableTest, workspace, testpattern string) ([]ti.RunnableTest, error) {
	files, err := getFiles(fmt.Sprintf("%s/**/%s", workspace, testpattern))
	if err != nil {
		return nil, err
	}

	for _, path := range files {
		if path == "" {
			continue
		}
		test := ti.RunnableTest{
			Class: path,
		}
		tests = append(tests, test)
	}
	return tests, nil
}

// GetPythonTests returns list of RunnableTests in the workspace with python extension.
// In case of errors, return empty list
func GetPythonTests(workspace string, testGlobs []string) []ti.RunnableTest {
	tests := make([]ti.RunnableTest, 0)
	tests, err := getPythonTestsFromPattern(tests, workspace, "test_*.py")
	if err != nil {
		return tests
	}
	tests, err = getPythonTestsFromPattern(tests, workspace, "*_test.py")
	if err != nil {
		return tests
	}

	return tests
}

// UnzipAndGetTestInfo unzips the Python agent zip file, and return a pair of
// string for script path and test harness command as test information.
// In case of errors, return a pair of empty string as test information.
func UnzipAndGetTestInfo(agentInstallDir string, ignoreInstr bool, testHarness string,
	userArgs string, log *logrus.Logger) (scriptPath, testHarnessCmd string, err error) {
	zip := archiver.Zip{
		OverwriteExisting: true,
	}
	// Unzip everything at agentInstallDir/python-agent.zip
	err = zip.Unarchive(filepath.Join(agentInstallDir, "python-agent.zip"), agentInstallDir)
	if err != nil {
		log.WithError(err).Println("could not unzip the python agent")
		return "", "", err
	}

	scriptPath = filepath.Join(agentInstallDir, "harness", "python-agent", "python_agent.py")
	log.Infoln(fmt.Sprintf("scriptPath: %s", scriptPath))

	testHarnessCmd = ""
	if !ignoreInstr {
		testHarnessCmd = strings.TrimSpace(fmt.Sprintf("\"%s %s\"", testHarness, userArgs))
		log.Infoln(fmt.Sprintf("testHarnessCmd: %s", testHarnessCmd))
	}
	return scriptPath, testHarnessCmd, nil
}
