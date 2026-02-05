// Copyright 2022 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package junit

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/harness/lite-engine/ti/report/parser/junit/gojunit"
	ti "github.com/harness/ti-client/types"
	"github.com/mattn/go-zglob"
	"github.com/sirupsen/logrus"
)

const (
	strMaxSize                  = 8000 // Keep the last 8k characters in each field.
	testIntelligenceSkipMessage = "Test skipped by Harness Test Intelligence."
)

const defaultRootSuiteName = "Root Suite"
const rootSuiteEnvVariableName = "HARNESS_JUNIT_ROOT_SUITE_NAME"

func getRootSuiteName(envs map[string]string) string {
	if val, ok := envs[rootSuiteEnvVariableName]; ok {
		return val
	}
	return defaultRootSuiteName
}

// ParseTests parses XMLs and writes relevant data to the channel
func ParseTests(paths []string, log *logrus.Logger, envs map[string]string) []*ti.TestCase {
	files := getFiles(paths, log)

	log.Debugln(fmt.Sprintf("list of files to collect test reports from: %s", files))
	if len(files) == 0 {
		log.Errorln("could not find any files matching the provided report path")
	}
	fileMap := make(map[string]int)
	overallCounts := TestCounts{}
	var tests []*ti.TestCase
	for _, file := range files {
		suites, err := gojunit.IngestFile(file, getRootSuiteName(envs))
		if err != nil {
			log.WithError(err).WithField("file", file).
				Errorln(fmt.Sprintf("could not parse file %s", file))
			continue
		}
		fileCounts := processTestSuites(&tests, suites)
		overallCounts.Total += fileCounts.Total
		overallCounts.Passed += fileCounts.Passed
		overallCounts.Failed += fileCounts.Failed
		overallCounts.Skipped += fileCounts.Skipped
		overallCounts.SkippedByTi += fileCounts.SkippedByTi
		overallCounts.Error += fileCounts.Error
		overallCounts.Unknown += fileCounts.Unknown
		fileMap[file] = fileCounts.Total
	}

	log.Infoln("Number of cases parsed in each file: ", fileMap)
	log.Infoln(fmt.Sprintf("parsed %d test cases", overallCounts.Total), "num_cases", overallCounts.Total)
	// Print formatted test report
	printTestReport(overallCounts, log)
	return tests
}

type TestCounts struct {
	Total       int
	Passed      int
	Failed      int
	Skipped     int
	SkippedByTi int
	Error       int
	Unknown     int
}

// processTestSuites recusively writes the test data from parsed data to the
// input channel and returns the total number of tests written to the channel
func processTestSuites(tests *[]*ti.TestCase, suites []gojunit.Suite) TestCounts {
	counts := TestCounts{}
	for _, suite := range suites { //nolint:gocritic
		for _, test := range suite.Tests { //nolint:gocritic
			ct := convert(test, suite)
			if ct.Name != "" {
				*tests = append(*tests, ct)
				counts.Total++
				// Count by status
				switch test.Result.Status {
				case ti.StatusPassed:
					counts.Passed++
				case ti.StatusFailed:
					counts.Failed++
				case ti.StatusSkipped:
					if test.Result.Message == testIntelligenceSkipMessage {
						counts.SkippedByTi++
					} else {
						counts.Skipped++
					}
				case ti.StatusError:
					counts.Error++
				default:
					// This is not printed for now
					counts.Unknown++
				}
			}
		}
		nestedCounts := processTestSuites(tests, suite.Suites)
		counts.Total += nestedCounts.Total
		counts.Passed += nestedCounts.Passed
		counts.Failed += nestedCounts.Failed
		counts.Skipped += nestedCounts.Skipped
		counts.SkippedByTi += nestedCounts.SkippedByTi
		counts.Error += nestedCounts.Error
		counts.Unknown += nestedCounts.Unknown
	}
	return counts
}

// getFiles returns uniques file paths provided in the input after expanding the input paths
func getFiles(paths []string, log *logrus.Logger) []string {
	var files []string
	for _, p := range paths {
		path, err := expandTilde(p)
		if err != nil {
			log.WithError(err).WithField("path", p).
				Errorln("errored while trying to expand paths")
			continue
		}
		matches, err := zglob.Glob(path)
		if err != nil {
			log.WithError(err).WithField("path", path).
				Errorln("errored while trying to resolve path regex")
			continue
		}

		files = append(files, matches...)
	}
	return uniqueItems(files)
}

func uniqueItems(items []string) []string {
	var result []string

	set := make(map[string]bool)
	for _, item := range items {
		if _, ok := set[item]; !ok {
			result = append(result, item)
			set[item] = true
		}
	}
	return result
}

// convert combines relevant information in test cases and test suites and parses it to our custom format
func convert(testCase gojunit.Test, testSuite gojunit.Suite) *ti.TestCase { //nolint:gocritic
	testCase.Result.Desc = restrictLength(testCase.Result.Desc)
	testCase.Result.Message = restrictLength(testCase.Result.Message)
	return &ti.TestCase{
		Name:       testCase.Name,
		SuiteName:  testSuite.Name,
		ClassName:  testCase.Classname,
		FileName:   testCase.Filename,
		DurationMs: testCase.DurationMs,
		Result:     testCase.Result,
		SystemOut:  restrictLength(testCase.SystemOut),
		SystemErr:  restrictLength(testCase.SystemErr),
	}
}

// printTestReport prints a formatted test report table
func printTestReport(counts TestCounts, log *logrus.Logger) {
	// Only print the report if there are tests to report
	if counts.Total == 0 {
		return
	}
	log.Info("\n================= Harness Test Report =================")
	// Determine overall status message
	if counts.Failed == 0 && counts.Error == 0 {
		log.Info("✓ All tests passed.")
	} else {
		log.Infof("✗ %d test(s) failed.", counts.Failed+counts.Error)
	}

	log.Info("+-----------+----------------------+---------+")

	if counts.Passed > 0 {
		log.Infof("| Passed    |                      | %6d  |", counts.Passed)
		log.Info("+-----------+----------------------+---------+")
	}

	if counts.Failed > 0 || counts.Error > 0 {
		log.Infof("| Failed    | total                | %6d  |", counts.Failed+counts.Error)
		log.Info("+-----------+----------------------+---------+")
	}

	if counts.Skipped > 0 || counts.SkippedByTi > 0 {
		if counts.Skipped > 0 {
			log.Infof("| Skipped   | by framework         | %6d  |", counts.Skipped)
			if counts.SkippedByTi > 0 {
				log.Infof("|           | by Test Intelligence | %6d  |", counts.SkippedByTi)
			}
		} else {
			log.Infof("| Skipped   | by Test Intelligence | %6d  |", counts.SkippedByTi)
		}
		log.Info("+-----------+----------------------+---------+")
	}

	// Total
	log.Infof("| TOTAL     | all tests            | %6d  |", counts.Total)
	log.Info("+-----------+----------------------+---------+")
	log.Info("")
}

// restrictLength trims string to last strMaxsize characters
func restrictLength(s string) string {
	if len(s) <= strMaxSize {
		return s
	}
	return s[len(s)-strMaxSize:]
}

// expandTilde method expands the given file path to include the home directory
// if the path is prefixed with `~`. If it isn't prefixed with `~`, the path is
// returned as-is.
func expandTilde(path string) (string, error) {
	if path == "" {
		return path, nil
	}

	if path[0] != '~' {
		return path, nil
	}

	if len(path) > 1 && path[1] != '/' && path[1] != '\\' {
		return "", errors.New("cannot expand user-specific home dir")
	}

	dir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to fetch home directory: %w", err)
	}

	return filepath.Join(dir, path[1:]), nil
}
