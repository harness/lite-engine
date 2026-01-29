// Copyright 2021 Harness Inc. All rights reserved.
// Use of this source code is governed by the PolyForm Free Trial 1.0.0 license
// that can be found in the licenses directory at the root of this repository, also available at
// https://polyformproject.org/wp-content/uploads/2020/05/PolyForm-Free-Trial-1.0.0.txt.

package ruby

import (
	"bufio"
	"errors"
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
	defaultTestGlobs   = []string{"**/spec/**/*_spec.rb"}
	filterExcludeGlobs = []string{"**/vendor/**/*.rb"}
)

const rspecJuintFormatterString string = "RspecJunitFormatter"

func getRubyTestsFromPattern(workspace string, testGlobs, excludeGlobs []string, log *logrus.Logger) []ti.RunnableTest {
	tests := make([]ti.RunnableTest, 0)
	// iterate over all the test globs
	for _, testGlob := range testGlobs {
		// find all the files matching the glob
		if !strings.HasPrefix(testGlob, "**") && !strings.HasPrefix(testGlob, "/") {
			testGlob = filepath.Join(workspace, testGlob)
		}
		matches, err := zglob.Glob(testGlob)
		if err != nil {
			log.Info(fmt.Sprintf("could not find ruby tests using %s: %s", testGlob, err))
		}
		// iterate over all the matches
		for _, match := range matches {
			// append a new RunnableTest to the tests slice if its a file
			if info, err := os.Stat(match); err == nil && !info.IsDir() && !matchedAny(match, excludeGlobs) {
				tests = append(tests, ti.RunnableTest{
					Class: match,
				})
			}
		}
	}

	return tests
}

func getRubyTestsFromPatternV2(workspace string, testGlobs, excludeGlobs []string, log *logrus.Logger) []ti.RunnableTest {
	tests := make([]ti.RunnableTest, 0)
	// iterate over all the test globs
	for _, testGlob := range testGlobs {
		// find all the files matching the glob
		testGlob = filepath.Join(workspace, testGlob)
		matches, err := zglob.Glob(testGlob)
		if err != nil {
			log.Info(fmt.Sprintf("could not find ruby tests using %s: %s", testGlob, err))
		}
		// iterate over all the matches
		for _, match := range matches {
			// append a new RunnableTest to the tests slice if its a file
			if info, err := os.Stat(match); err == nil && !info.IsDir() && !matchedAny(match, excludeGlobs) {
				tests = append(tests, ti.RunnableTest{
					Class: match,
				})
			}
		}
	}

	return tests
}

func matchedAny(class string, globs []string) bool {
	for _, glob := range globs {
		if matchedExclude, _ := zglob.Match(glob, class); matchedExclude {
			return true
		}
	}
	return false
}

// GetRubyTests returns list of RunnableTests in the workspace with python extension.
// In case of errors, return empty list
func GetRubyTests(workspace string, testGlobs, excludeGlobs []string, log *logrus.Logger) ([]ti.RunnableTest, error) {
	if len(testGlobs) == 0 {
		testGlobs = defaultTestGlobs
	}
	log.Infoln(fmt.Sprintf("testGlobs: %v", testGlobs))
	log.Infoln(fmt.Sprintf("workspace: %v", workspace))

	tests := getRubyTestsFromPattern(workspace, testGlobs, excludeGlobs, log)

	if len(tests) == 0 {
		return tests, fmt.Errorf("no ruby tests found with the given patterns %v", testGlobs)
	}
	return tests, nil
}

// GetRubyTestsV2 returns list of RunnableTests in the workspace with ruby extension.
// In case of errors, return empty list
func GetRubyTestsV2(workspace string, testGlobs, excludeGlobs []string, log *logrus.Logger) ([]ti.RunnableTest, error) {
	if len(testGlobs) == 0 {
		testGlobs = defaultTestGlobs
	}
	log.Infoln(fmt.Sprintf("testGlobs: %v", testGlobs))
	log.Infoln(fmt.Sprintf("workspace: %v", workspace))

	tests := getRubyTestsFromPatternV2(workspace, testGlobs, excludeGlobs, log)

	if len(tests) == 0 {
		return tests, fmt.Errorf("no ruby tests found with the given patterns %v", testGlobs)
	}
	return tests, nil
}

// UnzipAndGetTestInfo unzips the Ruby agent zip file, and return a
// string for script path as test information.
// In case of errors, return empty string as test information.
func UnzipAndGetTestInfo(agentInstallDir string, log *logrus.Logger) (scriptPath string, err error) {
	// Unzip everything at agentInstallDir/ruby-agent.zip
	err = common.ExtractArchive(filepath.Join(agentInstallDir, "ruby-agent.zip"), agentInstallDir)
	if err != nil {
		log.WithError(err).Println("could not unzip the ruby agent")
		return "", err
	}

	scriptPath = filepath.Join(agentInstallDir, "harness", "ruby-agent")
	log.Infoln(fmt.Sprintf("scriptPath: %s", scriptPath))

	return scriptPath, nil
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

// CheckFileForString checks if a file exists and contains a specific string
func CheckFileForString(filePath, targetString string) (bool, error) {
	file, err := os.Open(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil // File doesn't exist
		}
		return false, err // Other error occurred
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.Contains(line, targetString) {
			return true, nil // Target string found in the file
		}
	}
	if err := scanner.Err(); err != nil {
		return false, err // Error occurred while scanning the file
	}
	return false, nil // Target string not found in the file
}

// WriteRspecFile writes to the .rspec-local file
func WriteRspecFile(workspace, repoPath string, splitIdx int, disableJunitInstrumentation bool) error {
	scriptPath := filepath.Join(repoPath, "test_intelligence.rb")
	rspecLocalPath := filepath.Join(workspace, ".rspec-local")
	rspecPath := filepath.Join(workspace, ".rspec")
	juintPath := filepath.Join(workspace, fmt.Sprintf("rspec_%d.xml", splitIdx))

	// Open or create the .rspec-local file
	file, err := os.OpenFile(rspecLocalPath, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0644) //nolint:mnd
	if err != nil {
		return fmt.Errorf("failed to open .rspec-local file: %v", err)
	}
	defer file.Close()

	// Write the required line to the file
	if _, err = file.WriteString(fmt.Sprintf("--require %q\n", scriptPath)); err != nil { //nolint:gocritic // preferFprint: WriteString is intentional
		return fmt.Errorf("failed to write to agent path to .rspec-local file: %v", err)
	}

	if !disableJunitInstrumentation {
		existsInRspec, err := CheckFileForString(rspecPath, rspecJuintFormatterString)
		if err != nil {
			return fmt.Errorf("failed to check .rspec file for RspecJunitFormatter: %v", err)
		}
		existsInRspecLocal, err := CheckFileForString(rspecLocalPath, rspecJuintFormatterString)
		if err != nil {
			return fmt.Errorf("failed to check .rspec-local file for RspecJunitFormatter: %v", err)
		}

		if !existsInRspec && !existsInRspecLocal {
			// Write the required line to the file
			if _, err = file.WriteString(fmt.Sprintf("--format %s --out %s\n", rspecJuintFormatterString, juintPath)); err != nil { //nolint:gocritic // preferFprint: WriteString is intentional
				return fmt.Errorf("failed to write xml formatter to .rspec-local file: %v", err)
			}
		}
	}

	return nil
}

// GetRubyGlobs returns the globs if user specified, return default globs if not specified.
func GetRubyGlobs(testGlobs []string, envs map[string]string) (includeGlobs, excludeGlobs []string) {
	if len(testGlobs) == 0 {
		testGlobs = defaultTestGlobs
	}
	excludeGlobs = filterExcludeGlobs
	if envs["TI_SKIP_EXCLUDE_VENDOR"] == "true" {
		excludeGlobs = make([]string, 0)
	}
	return testGlobs, excludeGlobs
}

// prepend adds line in front of a file
func prepend(lineToAdd, fileName string) error {
	fileData, err := os.ReadFile(fileName)
	if err != nil {
		return err
	}

	newContent := []byte(lineToAdd + "\n" + string(fileData))
	err = os.WriteFile(fileName, newContent, os.ModePerm) //nolint:gosec // G306: Intentional - file needs broader permissions
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
