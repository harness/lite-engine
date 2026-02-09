// Copyright 2021 Harness Inc. All rights reserved.
// Use of this source code is governed by the PolyForm Free Trial 1.0.0 license
// that can be found in the licenses directory at the root of this repository, also available at
// https://polyformproject.org/wp-content/uploads/2020/05/PolyForm-Free-Trial-1.0.0.txt.

package python

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/harness/lite-engine/ti/instrumentation/common"
	ti "github.com/harness/ti-client/types"
	"github.com/mattn/go-zglob"

	"github.com/sirupsen/logrus"
)

var (
	defaultTestGlobs = []string{"**/test_*.py", "**/*_test.py"}
)

func getPythonTestsFromPattern(workspace string, testGlobs []string) ([]ti.RunnableTest, error) {
	tests := make([]ti.RunnableTest, 0)
	files, err := common.GetFiles(fmt.Sprintf("%s/**/*.py", workspace))
	if err != nil {
		return nil, err
	}

	for _, path := range files {
		if path == "" {
			continue
		}
		for _, glob := range testGlobs {
			if matched, _ := zglob.Match(glob, path); !matched {
				continue
			}
			test := ti.RunnableTest{
				Class: path,
			}
			tests = append(tests, test)
		}
	}
	return tests, nil
}

// GetPythonTests returns list of RunnableTests in the workspace with python extension.
// In case of errors, return empty list
func GetPythonTests(workspace string, testGlobs []string) []ti.RunnableTest {
	testGlobs, _ = GetPythonGlobs(testGlobs)
	tests, err := getPythonTestsFromPattern(workspace, testGlobs)
	if err != nil {
		return tests
	}

	return tests
}

// GetPythonGlobs returns the globs if user specified, return default globs if not specified.
func GetPythonGlobs(testGlobs []string) (includeGlobs, excludeGlobs []string) {
	if len(testGlobs) == 0 {
		testGlobs = defaultTestGlobs
	}
	return testGlobs, make([]string, 0)
}

// UnzipAndGetTestInfo unzips the Python agent zip file, and return a pair of
// string for script path and test harness command as test information.
// In case of errors, return a pair of empty string as test information.
func UnzipAndGetTestInfo(agentInstallDir string, ignoreInstr bool, testHarness string,
	userArgs string, log *logrus.Logger) (scriptPath, testHarnessCmd string, err error) {
	// Unzip everything at agentInstallDir/python-agent.zip
	err = common.ExtractArchive(filepath.Join(agentInstallDir, "python-agent.zip"), agentInstallDir)
	if err != nil {
		log.WithError(err).Println("could not unzip the python agent")
		return "", "", nil
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

// UnzipAndGetTestInfoV2 unzips the Python agent zip file, and return a pair of
// string for script path and test harness command as test information.
// In case of errors, return a pair of empty string as test information.
func UnzipAndGetTestInfoV2(agentInstallDir string, log *logrus.Logger) (scriptPath string, err error) {
	err = common.ExtractArchive(filepath.Join(agentInstallDir, "python-agent-v2.zip"), agentInstallDir)
	if err != nil {
		log.WithError(err).Println("could not unzip the python agent")
		return "", err
	}

	scriptPath = filepath.Join(agentInstallDir, "harness", "python-agent-v2")
	log.Infoln(fmt.Sprintf("scriptPath: %s", scriptPath))

	return scriptPath, nil
}

func FindWhlFile(folderPath string) (string, error) {
	files, err := os.ReadDir(folderPath)
	if err != nil {
		return "", err
	}

	for _, file := range files {
		if !file.IsDir() && strings.HasSuffix(strings.ToLower(file.Name()), ".whl") {
			return filepath.Join(folderPath, file.Name()), nil
		}
	}

	return "", fmt.Errorf("no .whl file found in the folder")
}

func FindPyPluginFile(folderPath string) (string, error) {
	files, err := os.ReadDir(folderPath)
	if err != nil {
		return "", err
	}

	for _, file := range files {
		if !file.IsDir() && strings.HasSuffix(strings.ToLower(file.Name()), ".py") && strings.HasPrefix(strings.ToLower(file.Name()), "harness_ti_pytest_plugin") {
			return filepath.Join(folderPath, file.Name()), nil
		}
	}

	return "", fmt.Errorf("py plugin file not found in the folder")
}
