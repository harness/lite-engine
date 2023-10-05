// Copyright 2021 Harness Inc. All rights reserved.
// Use of this source code is governed by the PolyForm Free Trial 1.0.0 license
// that can be found in the licenses directory at the root of this repository, also available at
// https://polyformproject.org/wp-content/uploads/2020/05/PolyForm-Free-Trial-1.0.0.txt.

package ruby

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/harness/lite-engine/ti/instrumentation/common"
	ti "github.com/harness/ti-client/types"
	"github.com/mattn/go-zglob"

	"github.com/mholt/archiver/v3"
	"github.com/sirupsen/logrus"
)

var (
	defaultTestGlobs = []string{"*_spec.rb"}
)

func getRubyTestsFromPattern(workspace string, testGlobs []string) ([]ti.RunnableTest, error) {
	tests := make([]ti.RunnableTest, 0)
	files, err := common.GetFiles(fmt.Sprintf("%s/**/*.rb", workspace))
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

// GetRubyTests returns list of RunnableTests in the workspace with python extension.
// In case of errors, return empty list
func GetRubyTests(workspace string, testGlobs []string) []ti.RunnableTest {
	if len(testGlobs) == 0 {
		testGlobs = defaultTestGlobs
	}
	tests, err := getRubyTestsFromPattern(workspace, testGlobs)
	if err != nil {
		return tests
	}

	return tests
}

// UnzipAndGetTestInfo unzips the Python agent zip file, and return a pair of
// string for script path and test harness command as test information.
// In case of errors, return a pair of empty string as test information.
func UnzipAndGetTestInfo(agentInstallDir string, log *logrus.Logger) (scriptPath string, err error) {
	zip := archiver.Zip{
		OverwriteExisting: true,
	}
	// Unzip everything at agentInstallDir/ruby-agent.zip
	err = zip.Unarchive(filepath.Join(agentInstallDir, "ruby-agent.zip"), agentInstallDir)
	if err != nil {
		log.WithError(err).Println("could not unzip the ruby agent")
		return "", err
	}

	scriptPath = filepath.Join(agentInstallDir, "harness", "ruby-agent")
	log.Infoln(fmt.Sprintf("scriptPath: %s", scriptPath))

	return scriptPath, nil
}

func WriteGemFile(repoPath string) error {
	f, err := os.OpenFile("Gemfile", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644) //nolint:gomnd
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(fmt.Sprintf("gem 'harness_ruby_agent', path: '%s'", repoPath))
	if err != nil {
		return err
	}
	return nil
}

// WriteHelperFile writes the rspec helper file needed to attach agent.
// If no rspec helper file found in this pattern or any error happens,
// will print a message ask for manual write and continue
func WriteHelperFile(repoPath string) error {
	pattern := "**/*spec_helper*.rb"

	matches, err := filepath.Glob(pattern)
	if err != nil {
		return err
	}
	if len(matches) == 0 {
		return errors.New("cannot find rspec helper file. Please make change manually to enable TI")
	}

	f, err := os.OpenFile(findRootMostPath(matches), os.O_APPEND|os.O_WRONLY, 0644) //nolint:gomnd
	if err != nil {
		return err
	}
	defer f.Close()
	scriptPath := filepath.Join(repoPath, "test_intelligence.rb")
	_, err = f.WriteString(fmt.Sprintf("\nrequire '%s'", scriptPath))
	if err != nil {
		return err
	}
	return nil
}

// findRootMostPath helper function to find shortest file path
func findRootMostPath(paths []string) string {
	rootmost := paths[0]
	for _, path := range paths[1:] {
		if len(path) < len(rootmost) {
			rootmost = path
		}
	}
	return rootmost
}
