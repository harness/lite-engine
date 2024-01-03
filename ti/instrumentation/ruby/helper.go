// Copyright 2021 Harness Inc. All rights reserved.
// Use of this source code is governed by the PolyForm Free Trial 1.0.0 license
// that can be found in the licenses directory at the root of this repository, also available at
// https://polyformproject.org/wp-content/uploads/2020/05/PolyForm-Free-Trial-1.0.0.txt.

package ruby

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	ti "github.com/harness/ti-client/types"
	"github.com/mattn/go-zglob"

	"github.com/mholt/archiver/v3"
	"github.com/sirupsen/logrus"
)

var (
	defaultTestGlobs = []string{"spec/**{,/*/**}/*_spec.rb"}
)

func getRubyTestsFromPattern(workspace string, testGlobs []string, log *logrus.Logger) []ti.RunnableTest {
	tests := make([]ti.RunnableTest, 0)
	// iterate over all the test globs
	for _, testGlob := range testGlobs {
		// find all the files matching the glob
		matches, err := zglob.Glob(filepath.Join(workspace, testGlob))
		if err != nil {
			log.Info(fmt.Sprintf("could not find ruby tests using %s: %s", testGlob, err))
		}
		// iterate over all the matches
		for _, match := range matches {
			// append a new RunnableTest to the tests slice if its a file
			if info, err := os.Stat(match); err == nil && !info.IsDir() {
				tests = append(tests, ti.RunnableTest{
					Class: match,
				})
			}
		}
	}

	return tests
}

// GetRubyTests returns list of RunnableTests in the workspace with python extension.
// In case of errors, return empty list
func GetRubyTests(workspace string, testGlobs []string, log *logrus.Logger) ([]ti.RunnableTest, error) {
	if len(testGlobs) == 0 {
		testGlobs = defaultTestGlobs
	}
	log.Infoln(fmt.Sprintf("testGlobs: %v", testGlobs))
	tests := getRubyTestsFromPattern(workspace, testGlobs, log)

	if len(tests) == 0 {
		return tests, fmt.Errorf("no ruby tests found with the given patterns %v", testGlobs)
	}
	return tests, nil
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

func AddHarnessRubyAgentToGemfile(workspace, repoPath string, log *logrus.Logger) error {
	c := fmt.Sprintf("cd %s; bundle add harness_ruby_agent --path %q --version ~> 0.0.1", workspace, repoPath) // #nosec
	cmdArgs := []string{"-c", c}
	cmd := exec.Command("sh", cmdArgs...)
	err := cmd.Run()

	if err != nil {
		log.WithError(err).Println("Error adding harness_ruby_agent gem")
		return err
	}
	log.Infoln("'harness_ruby_agent' successfully added and installed!")
	return nil
}

func AddRspecJunitFormatterToGemfile(workspace, repoPath string, log *logrus.Logger) error {
	c := fmt.Sprintf("cd %s; bundle add rspec_junit_formatter", workspace) // #nosec
	cmdArgs := []string{"-c", c}
	cmd := exec.Command("sh", cmdArgs...)
	err := cmd.Run()
	if err != nil {
		log.WithError(err).Println("Error adding rspec_junit_formatter gem")
		return err
	}
	log.Infoln("'rspec_junit_formatter' successfully added and installed!")
	return nil
}

// WriteHelperFile writes the rspec helper file needed to attach agent.
// If no rspec helper file found in this pattern or any error happens,
// will print a message ask for manual write and continue
func WriteHelperFile(workspace, repoPath string) error {
	pattern := fmt.Sprintf("%s/**/*spec_helper*.rb", workspace)

	matches, err := zglob.Glob(pattern)
	if err != nil {
		return err
	}
	if len(matches) == 0 {
		return errors.New("cannot find rspec helper file. Please make change manually to enable TI")
	}

	fileName := findRootMostPath(matches)
	scriptPath := filepath.Join(repoPath, "test_intelligence.rb")
	lineToAdd := fmt.Sprintf("require '%s'", scriptPath)

	err = prepend(lineToAdd, fileName)
	if err != nil {
		return err
	}
	return nil
}

// prepend adds line in front of a file
func prepend(lineToAdd, fileName string) error {
	fileData, err := os.ReadFile(fileName)
	if err != nil {
		return err
	}

	newContent := []byte(lineToAdd + "\n" + string(fileData))
	err = os.WriteFile(fileName, newContent, os.ModePerm)
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
