// Copyright 2022 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package junit

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/harness/lite-engine/ti/report/parser/junit/gojunit"
	ti "github.com/harness/ti-client/types"
	"github.com/mattn/go-zglob"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

const (
	strMaxSize = 8000 // Keep the last 8k characters in each field.
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
// If envFile is provided, exports test statistics to that file
func ParseTests(paths []string, log *logrus.Logger, envs map[string]string, envFile string) []*ti.TestCase {
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
		overallCounts.Error += fileCounts.Error
		overallCounts.Unknown += fileCounts.Unknown
		fileMap[file] = fileCounts.Total
	}

	log.Infoln("Number of cases parsed in each file: ", fileMap)
	log.Infoln(fmt.Sprintf("parsed %d test cases", overallCounts.Total), "num_cases", overallCounts.Total)
	// Print formatted test report
	printTestReport(overallCounts, log)

	// Export statistics if envFile is provided
	if envFile != "" {
		ExportTestStatistics(tests, overallCounts, envFile, log)
	}

	return tests
}

type TestCounts struct {
	Total  int
	Passed int

	Failed  int
	Skipped int
	Error   int
	Unknown int
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
				counts.Total += 1
				// Count by status
				switch test.Result.Status {
				case ti.StatusPassed:
					counts.Passed++
				case ti.StatusFailed:
					counts.Failed++
				case ti.StatusSkipped:
					counts.Skipped++
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
		log.Info("✔ All tests passed.")
	} else {
		log.Infof("✗ %d test(s) failed.", counts.Failed+counts.Error)
	}
	log.Info("+-----------+----------------+------+")
	if counts.Passed > 0 {
		log.Infof("| Passed    |                | %3d  |", counts.Passed)
		// log.Infof("|           | on retry       | %3d  |", 0) // No retry info available
		log.Info("+-----------+----------------+------+")
	}
	if counts.Failed > 0 || counts.Error > 0 {
		log.Infof("| Failed    |                | %3d  |", counts.Failed+counts.Error)
		log.Info("+-----------+----------------+------+")
	}
	if counts.Skipped > 0 {
		// TI will map this information in later iteration
		// log.Infof("| Skipped   | by TI logic    | %3d  |", 0) // No TI logic info available
		log.Infof("| Skipped   |                | %3d  |", counts.Skipped)
		log.Info("+-----------+----------------+------+")
	}
	// Total
	log.Infof("| TOTAL     | all tests      | %3d  |", counts.Total)
	log.Info("+-----------+----------------+------+")
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
		return "", errors.Wrap(err, "failed to fetch home directory")
	}

	return filepath.Join(dir, path[1:]), nil
}

// ExportTestStatistics writes test statistics to an environment file that can be consumed by fetchExportedVarsFromEnvFile
// This function is safe and will not return errors - it logs any failures instead
func ExportTestStatistics(tests []*ti.TestCase, counts TestCounts, envFilePath string, log *logrus.Logger) {
	// Calculate additional statistics
	totalDurationMs := int64(0)
	slowTests := make([]*ti.TestCase, 0)

	// Process each test case
	for _, test := range tests {
		totalDurationMs += test.DurationMs
		slowTests = append(slowTests, test)
	}

	sort.Slice(slowTests, func(i, j int) bool {
		return slowTests[i].DurationMs > slowTests[j].DurationMs
	})

	// Get top 5 slowest tests in JSON array format
	topFiveSlowests := make([]string, 0, 5)
	for i := 0; i < len(slowTests) && i < 5; i++ {
		testName := slowTests[i].Name
		if slowTests[i].ClassName != "" {
			testName = slowTests[i].ClassName + "#" + testName
		}
		durationSeconds := slowTests[i].DurationMs / 1000
		if slowTests[i].DurationMs%1000 != 0 {
			durationSeconds++
		}
		topFiveSlowests = append(topFiveSlowests, fmt.Sprintf("\"%s: %ds\"", testName, durationSeconds))
	}

	// Format as JSON array
	topFiveSlowestsJSON := "[" + strings.Join(topFiveSlowests, ", ") + "]"
	if len(topFiveSlowests) == 0 {
		topFiveSlowestsJSON = "[]"
	}

	// Calculate failed ratio
	failedRatio := 0.0
	if counts.Total > 0 {
		failedRatio = float64(counts.Failed+counts.Error) / float64(counts.Total)
	}

	// Create environment variables map
	envVars := map[string]string{
		"total_tests":            strconv.Itoa(counts.Total),
		"executed_count":         strconv.Itoa(counts.Total),
		"passed_count":           strconv.Itoa(counts.Passed),
		"failed_count":           strconv.Itoa(counts.Failed + counts.Error),
		"skipped_count":          strconv.Itoa(counts.Skipped),
		"failed_ratio":           fmt.Sprintf("%.4f", failedRatio),
		"duration_ms_total":      strconv.FormatInt(totalDurationMs, 10),
		"top_five_slowest_tests": topFiveSlowestsJSON,
	}

	// Write to environment file
	file, err := os.Create(envFilePath)
	if err != nil {
		log.WithError(err).WithField("envFilePath", envFilePath).Errorln("failed to create env file for test statistics")
		return
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			log.WithError(closeErr).WithField("envFilePath", envFilePath).Warnln("failed to close env file")
		}
	}()

	writer := bufio.NewWriter(file)
	defer func() {
		if flushErr := writer.Flush(); flushErr != nil {
			log.WithError(flushErr).WithField("envFilePath", envFilePath).Warnln("failed to flush env file")
		}
	}()

	// Write each environment variable in KEY=VALUE format
	for key, value := range envVars {
		_, err := writer.WriteString(fmt.Sprintf("%s=%s\n", key, value))
		if err != nil {
			log.WithError(err).WithField("envFilePath", envFilePath).WithField("key", key).Errorln("failed to write variable to env file")
			return
		}
	}

	log.WithField("envFilePath", envFilePath).Infoln("successfully exported test statistics to env file")
}
