// Copyright 2022 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package junit

import (
	"fmt"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/harness/lite-engine/ti/report/parser/junit/gojunit"
	ti "github.com/harness/ti-client/types"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

var (
	prefix  = "junit_test_999/"
	report1 = "testdata/reportWithPassFail.xml"
	report2 = "testdata/reportWithSkipError.xml"
	report3 = "testdata/reportWithNestedTestSuite.xml"
)

func getBaseDir() string {
	wd, _ := os.Getwd()
	fmt.Println("Working directory is: ", wd)
	return wd + prefix
}

// createNestedDir will create a nested directory relative to default temp directory
func createNestedDir(path string) error { //nolint:unparam
	absPath := getBaseDir() + path
	err := os.MkdirAll(absPath, 0777)
	if err != nil {
		return fmt.Errorf("could not create directory structure for testing: %s", err)
	}
	return nil
}

// removeBaseDir will clean up the temp directory
func removeBaseDir() error {
	err := os.RemoveAll(getBaseDir())
	if err != nil {
		return err
	}
	return nil
}

// copy file from src to relative dst in temp directory. Any existing file will be overwritten.
func copy(src, relDst string) error { //nolint:gocritic
	dst := getBaseDir() + relDst
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	if err != nil {
		return err
	}
	return out.Close()
}

func expectedPassedTest() *ti.TestCase {
	return &ti.TestCase{
		Name:      "report1test1",
		ClassName: "report1test1class",
		SuiteName: "report1",
		FileName:  "path/to/test/t1.java",
		Result: ti.Result{
			Status: ti.StatusPassed,
		},
		DurationMs: 123000,
		SystemOut:  "report1test1stdout",
		SystemErr:  "report1test1stderr",
	}
}

func expectedFailedTest() *ti.TestCase {
	return &ti.TestCase{
		Name:      "report1test2",
		ClassName: "report1test2class",
		SuiteName: "report1",
		FileName:  "path/to/test/t2.java",
		Result: ti.Result{
			Status:  ti.StatusFailed,
			Message: "report1test2message",
			Type:    "report1test2type",
			Desc:    "report1test2description",
		},
		DurationMs: 11000,
		SystemOut:  "report1test2stdout",
		SystemErr:  "report1test2stderr",
	}
}

func expectedSkippedTest() *ti.TestCase {
	return &ti.TestCase{
		Name:      "report2test1",
		ClassName: "report2test1class",
		SuiteName: "report2",
		Result: ti.Result{
			Status:  ti.StatusSkipped,
			Message: "report2test1message",
			Desc:    "report2test1description",
		},
		DurationMs: 123000,
		SystemOut:  "report2test1stdout",
		SystemErr:  "report2test1stderr",
	}
}

func expectedErrorTest() *ti.TestCase {
	return &ti.TestCase{
		Name:      "report2test2",
		ClassName: "report2test2class",
		SuiteName: "report2",
		Result: ti.Result{
			Status:  ti.StatusError,
			Message: "report2test2message",
			Type:    "report2test2type",
			Desc:    "report2test2description",
		},
		DurationMs: 11000,
		SystemOut:  "report2test2stdout",
		SystemErr:  "report2test2stderr",
	}
}

func expectedNestedTests() []*ti.TestCase {
	test1 := &ti.TestCase{
		Name:      "test1",
		ClassName: "t.st.c.ApiControllerTest",
		FileName:  "/harness/tests/unit/Controller/ApiControllerTest.php",
		SuiteName: "t\\st\\c\\ApiControllerTest",
		Result: ti.Result{
			Status: ti.StatusPassed,
		},
		DurationMs: 1000,
	}

	test2 := &ti.TestCase{
		Name:      "test17",
		ClassName: "t.st.c.ApiControllerTest",
		SuiteName: "t\\st\\c\\ApiControllerTest",
		FileName:  "/harness/tests/unit/Controller/ApiControllerTest.php",
		Result: ti.Result{
			Status: ti.StatusPassed,
		},
		DurationMs: 1000,
	}

	test3 := &ti.TestCase{
		Name:      "test20",
		ClassName: "t.st.c.RedirectControllerTest",
		FileName:  "/harness/tests/unit/Controller/RedirectControllerTest.php",
		SuiteName: "t\\st\\c\\RedirectControllerTest",
		Result: ti.Result{
			Status: ti.StatusPassed,
		},
		DurationMs: 2000,
	}

	test4 := &ti.TestCase{
		Name:      "test29",
		ClassName: "t.st.c.RouteDispatcherTest",
		FileName:  "/harness/tests/unit/RouteDispatcherTest.php",
		SuiteName: "t\\st\\c\\RouteDispatcherTest",
		Result: ti.Result{
			Status: ti.StatusPassed,
		},
		DurationMs: 2000,
	}

	test5 := &ti.TestCase{
		Name:      "test40",
		ClassName: "t.st.c.PdoAdapterTest",
		FileName:  "/harness/tests/unit/Storage/Adapter/PdoAdapterTest.php",
		SuiteName: "t\\st\\c\\PdoAdapterTest",
		Result: ti.Result{
			Status: ti.StatusPassed,
		},
		DurationMs: 2000,
	}
	return []*ti.TestCase{test1, test2, test3, test4, test5}
}

func TestGetTests_All(t *testing.T) {
	err := createNestedDir("a/b/c/d")
	if err != nil {
		t.Fatal(err)
	}
	err = copy(report1, "a/b/report1.xml")
	if err != nil {
		t.Fatal(err)
	}
	err = copy(report2, "a/b/c/d/report2.xml")
	if err != nil {
		t.Fatal(err)
	}
	err = copy(report3, "a/b/report3.xml")
	if err != nil {
		t.Fatal(err)
	}
	defer removeBaseDir() //nolint:errcheck
	var paths []string
	paths = append(paths, getBaseDir()+"**/*.xml") // Regex to get all reports
	envs := make(map[string]string)

	tests := ParseTests(paths, logrus.New(), envs, "")
	exp := []*ti.TestCase{expectedPassedTest(), expectedErrorTest(), expectedFailedTest(), expectedSkippedTest()}
	exp = append(exp, expectedNestedTests()...)
	assert.ElementsMatch(t, exp, tests)
}

func TestGetTests_All_MultiplePaths(t *testing.T) {
	err := createNestedDir("a/b/c/d")
	if err != nil {
		t.Fatal(err)
	}
	err = copy(report1, "a/b/report1.xml")
	if err != nil {
		t.Fatal(err)
	}
	err = copy(report2, "a/b/c/d/report2.xml")
	if err != nil {
		t.Fatal(err)
	}
	defer removeBaseDir() //nolint:errcheck
	basePath := getBaseDir()
	var paths []string
	// Add multiple paths to get repeated filenames. Tests should still be unique (per filename)
	paths = append(paths, basePath+"a/*/*.xml", basePath+"a/**/*.xml", basePath+"a/b/c/d/*.xml") // Regex to get both reports
	envs := make(map[string]string)

	tests := ParseTests(paths, logrus.New(), envs, "")
	exp := []*ti.TestCase{expectedPassedTest(), expectedErrorTest(), expectedFailedTest(), expectedSkippedTest()}
	assert.ElementsMatch(t, exp, tests)
}

func TestGetTests_FirstRegex(t *testing.T) {
	err := createNestedDir("a/b/c/d")
	if err != nil {
		t.Fatal(err)
	}
	err = copy(report1, "a/b/report1.xml")
	if err != nil {
		t.Fatal(err)
	}
	err = copy(report2, "a/b/c/d/report2.xml")
	if err != nil {
		t.Fatal(err)
	}
	defer removeBaseDir() //nolint:errcheck
	basePath := getBaseDir()
	var paths []string
	paths = append(paths, basePath+"a/b/*.xml") // Regex to get both reports
	envs := make(map[string]string)

	tests := ParseTests(paths, logrus.New(), envs, "")
	exp := []*ti.TestCase{expectedPassedTest(), expectedFailedTest()}
	assert.ElementsMatch(t, exp, tests)
}

func TestGetTests_SecondRegex(t *testing.T) {
	err := createNestedDir("a/b/c/d")
	if err != nil {
		t.Fatal(err)
	}
	err = copy(report1, "a/b/report1.xml")
	if err != nil {
		t.Fatal(err)
	}
	err = copy(report2, "a/b/c/d/report2.xml")
	if err != nil {
		t.Fatal(err)
	}
	defer removeBaseDir() //nolint:errcheck
	basePath := getBaseDir()
	var paths []string
	paths = append(paths, basePath+"a/b/**/*2.xml") // Regex to get both reports
	envs := make(map[string]string)

	tests := ParseTests(paths, logrus.New(), envs, "")
	exp := []*ti.TestCase{expectedSkippedTest(), expectedErrorTest()}
	assert.ElementsMatch(t, exp, tests)
}

func TestGetTests_NoMatchingRegex(t *testing.T) {
	err := createNestedDir("a/b/c/d")
	if err != nil {
		t.Fatal(err)
	}
	err = copy(report1, "a/b/report1.xml")
	if err != nil {
		t.Fatal(err)
	}
	err = copy(report2, "a/b/c/d/report2.xml")
	if err != nil {
		t.Fatal(err)
	}
	defer removeBaseDir() //nolint:errcheck
	basePath := getBaseDir()
	var paths []string
	paths = append(paths, basePath+"a/b/**/*3.xml") // Regex to get both reports
	envs := make(map[string]string)

	tests := ParseTests(paths, logrus.New(), envs, "")
	exp := []*ti.TestCase{}
	assert.ElementsMatch(t, exp, tests)
}

func Test_GetRootSuiteName(t *testing.T) {
	type args struct {
		envs map[string]string
	}
	tests := []struct {
		name string
		args args
		want string
	}{
		{
			name: "Root suite name provided in environment variable",
			args: args{envs: map[string]string{rootSuiteEnvVariableName: "CustomRootSuite"}},
			want: "CustomRootSuite",
		},
		{
			name: "No root suite name in environment variable, use default",
			args: args{envs: map[string]string{}},
			want: defaultRootSuiteName,
		},
		// Add more test cases as needed
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := getRootSuiteName(tt.args.envs); got != tt.want {
				t.Errorf("getRootSuiteName() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestProcessTestSuites(t *testing.T) {
	tests := []struct {
		name           string
		suites         []gojunit.Suite
		expectedCounts TestCounts
	}{
		{
			name: "Single suite with mixed results",
			suites: []gojunit.Suite{
				{
					Name: "TestSuite1",
					Tests: []gojunit.Test{
						{
							Name:     "test1",
							Result:   ti.Result{Status: ti.StatusPassed},
							Filename: "test1.go",
						},
						{
							Name:     "test2",
							Result:   ti.Result{Status: ti.StatusFailed},
							Filename: "test2.go",
						},
						{
							Name:     "test3",
							Result:   ti.Result{Status: ti.StatusSkipped},
							Filename: "test3.go",
						},
					},
				},
			},
			expectedCounts: TestCounts{
				Total:   3,
				Passed:  1,
				Failed:  1,
				Skipped: 1,
				Error:   0,
				Unknown: 0,
			},
		},
		{
			name: "Multiple suites",
			suites: []gojunit.Suite{
				{
					Name: "TestSuite1",
					Tests: []gojunit.Test{
						{
							Name:     "test1",
							Result:   ti.Result{Status: ti.StatusPassed},
							Filename: "test1.go",
						},
						{
							Name:     "test2",
							Result:   ti.Result{Status: ti.StatusError},
							Filename: "test2.go",
						},
					},
				},
				{
					Name: "TestSuite2",
					Tests: []gojunit.Test{
						{
							Name:     "test3",
							Result:   ti.Result{Status: ti.StatusPassed},
							Filename: "test3.go",
						},
					},
				},
			},
			expectedCounts: TestCounts{
				Total:   3,
				Passed:  2,
				Failed:  0,
				Skipped: 0,
				Error:   1,
				Unknown: 0,
			},
		},
		{
			name: "Nested suites",
			suites: []gojunit.Suite{
				{
					Name: "ParentSuite",
					Tests: []gojunit.Test{
						{
							Name:     "parentTest",
							Result:   ti.Result{Status: ti.StatusPassed},
							Filename: "parent.go",
						},
					},
					Suites: []gojunit.Suite{
						{
							Name: "ChildSuite",
							Tests: []gojunit.Test{
								{
									Name:     "childTest",
									Result:   ti.Result{Status: ti.StatusFailed},
									Filename: "child.go",
								},
							},
						},
					},
				},
			},
			expectedCounts: TestCounts{
				Total:   2,
				Passed:  1,
				Failed:  1,
				Skipped: 0,
				Error:   0,
				Unknown: 0,
			},
		},
		{
			name: "Empty test name should be ignored",
			suites: []gojunit.Suite{
				{
					Name: "TestSuite1",
					Tests: []gojunit.Test{
						{
							Name:     "validTest",
							Result:   ti.Result{Status: ti.StatusPassed},
							Filename: "test.go",
						},
						{
							Name:     "", // Empty name should be ignored
							Result:   ti.Result{Status: ti.StatusPassed},
							Filename: "test.go",
						},
					},
				},
			},
			expectedCounts: TestCounts{
				Total:   1,
				Passed:  1,
				Failed:  0,
				Skipped: 0,
				Error:   0,
				Unknown: 0,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a test slice
			var testCases []*ti.TestCase
			// Call the function
			counts := processTestSuites(&testCases, tt.suites)
			// Verify counts
			assert.Equal(t, tt.expectedCounts.Total, counts.Total, "Total count mismatch")
			assert.Equal(t, tt.expectedCounts.Passed, counts.Passed, "Passed count mismatch")
			assert.Equal(t, tt.expectedCounts.Failed, counts.Failed, "Failed count mismatch")
			assert.Equal(t, tt.expectedCounts.Skipped, counts.Skipped, "Skipped count mismatch")
			assert.Equal(t, tt.expectedCounts.Error, counts.Error, "Error count mismatch")
			// Verify that the correct number of test cases were sent to the channel
			assert.Equal(t, tt.expectedCounts.Total, len(testCases), "Number of test cases sent to channel should match total count")
		})
	}
}

func TestExportTestStatistics(t *testing.T) {
	tests := []struct {
		name          string
		testCases     []*ti.TestCase
		counts        TestCounts
		expectedVars  map[string]string
		expectedError bool
	}{
		{
			name: "Basic test statistics export",
			testCases: []*ti.TestCase{
				{
					Name:       "testLogin",
					ClassName:  "UserTest",
					DurationMs: 5000, // 5 seconds
					Result:     ti.Result{Status: ti.StatusPassed},
				},
				{
					Name:       "testLogout",
					ClassName:  "UserTest",
					DurationMs: 3000, // 3 seconds
					Result:     ti.Result{Status: ti.StatusFailed},
				},
				{
					Name:       "testSkipped",
					ClassName:  "UserTest",
					DurationMs: 1000, // 1 second
					Result:     ti.Result{Status: ti.StatusSkipped},
				},
			},
			counts: TestCounts{
				Total:   3,
				Passed:  1,
				Failed:  1,
				Skipped: 1,
				Error:   0,
			},
			expectedVars: map[string]string{
				"total_tests":            "3",
				"executed_count":         "3",
				"passed_count":           "1",
				"failed_count":           "1",
				"skipped_count":          "1",
				"failed_ratio":           "0.3333",
				"duration_ms_total":      "9000",
				"top_five_slowest_tests": "[\"UserTest#testLogin: 5s\", \"UserTest#testLogout: 3s\", \"UserTest#testSkipped: 1s\"]",
			},
			expectedError: false,
		},
		{
			name:      "Empty test cases",
			testCases: []*ti.TestCase{},
			counts: TestCounts{
				Total:   0,
				Passed:  0,
				Failed:  0,
				Skipped: 0,
				Error:   0,
			},
			expectedVars: map[string]string{
				"total_tests":            "0",
				"executed_count":         "0",
				"passed_count":           "0",
				"failed_count":           "0",
				"skipped_count":          "0",
				"failed_ratio":           "0.0000",
				"duration_ms_total":      "0",
				"top_five_slowest_tests": "[]",
			},
			expectedError: false,
		},
		{
			name: "Test with millisecond rounding",
			testCases: []*ti.TestCase{
				{
					Name:       "testFast",
					ClassName:  "FastTest",
					DurationMs: 1500, // 1.5 seconds, should round up to 2s
					Result:     ti.Result{Status: ti.StatusPassed},
				},
				{
					Name:       "testExact",
					ClassName:  "FastTest",
					DurationMs: 2000, // Exact 2 seconds
					Result:     ti.Result{Status: ti.StatusPassed},
				},
			},
			counts: TestCounts{
				Total:   2,
				Passed:  2,
				Failed:  0,
				Skipped: 0,
				Error:   0,
			},
			expectedVars: map[string]string{
				"total_tests":            "2",
				"executed_count":         "2",
				"passed_count":           "2",
				"failed_count":           "0",
				"skipped_count":          "0",
				"failed_ratio":           "0.0000",
				"duration_ms_total":      "3500",
				"top_five_slowest_tests": "[\"FastTest#testExact: 2s\", \"FastTest#testFast: 2s\"]",
			},
			expectedError: false,
		},
		{
			name: "Test without class name",
			testCases: []*ti.TestCase{
				{
					Name:       "standaloneTest",
					ClassName:  "", // No class name
					DurationMs: 4000,
					Result:     ti.Result{Status: ti.StatusPassed},
				},
			},
			counts: TestCounts{
				Total:   1,
				Passed:  1,
				Failed:  0,
				Skipped: 0,
				Error:   0,
			},
			expectedVars: map[string]string{
				"total_tests":            "1",
				"executed_count":         "1",
				"passed_count":           "1",
				"failed_count":           "0",
				"skipped_count":          "0",
				"failed_ratio":           "0.0000",
				"duration_ms_total":      "4000",
				"top_five_slowest_tests": "[\"standaloneTest: 4s\"]",
			},
			expectedError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a temporary file for testing
			tempFile, err := os.CreateTemp("", "test_stats_*.env")
			assert.NoError(t, err)
			defer os.Remove(tempFile.Name())
			tempFile.Close()

			// Call ExportTestStatistics with a test logger
			testLogger := logrus.New()
			testLogger.SetOutput(io.Discard) // Suppress log output during tests
			ExportTestStatistics(tt.testCases, tt.counts, tempFile.Name(), testLogger)

			if tt.expectedError {
				// For error cases, check if file was not created or is empty
				if _, err := os.Stat(tempFile.Name()); os.IsNotExist(err) {
					return // File doesn't exist, which is expected for error cases
				}
				content, _ := os.ReadFile(tempFile.Name())
				if len(content) == 0 {
					return // File is empty, which is expected for error cases
				}
			}

			// Read the generated file
			content, err := os.ReadFile(tempFile.Name())
			assert.NoError(t, err)

			// Parse the content and verify each expected variable
			lines := strings.Split(string(content), "\n")
			actualVars := make(map[string]string)
			for _, line := range lines {
				line = strings.TrimSpace(line)
				if line == "" {
					continue
				}
				parts := strings.SplitN(line, "=", 2)
				if len(parts) == 2 {
					actualVars[parts[0]] = parts[1]
				}
			}

			// Verify all expected variables are present and correct
			for key, expectedValue := range tt.expectedVars {
				actualValue, exists := actualVars[key]
				assert.True(t, exists, "Expected variable %s not found", key)
				assert.Equal(t, expectedValue, actualValue, "Variable %s has incorrect value", key)
			}

			// Verify no unexpected variables are present
			assert.Equal(t, len(tt.expectedVars), len(actualVars), "Number of variables mismatch")
		})
	}
}

func TestExportTestStatistics_InvalidPath(t *testing.T) {
	// Test with invalid file path
	testCases := []*ti.TestCase{
		{
			Name:       "test",
			DurationMs: 1000,
			Result:     ti.Result{Status: ti.StatusPassed},
		},
	}
	counts := TestCounts{Total: 1, Passed: 1}

	// Create a logger to capture error logs
	testLogger := logrus.New()
	var logOutput strings.Builder
	testLogger.SetOutput(&logOutput)

	// Try to write to an invalid path - function should not panic, just log error
	ExportTestStatistics(testCases, counts, "/invalid/path/that/does/not/exist.env", testLogger)

	// Verify that an error was logged
	logContent := logOutput.String()
	assert.Contains(t, logContent, "failed to create env file")
	assert.Contains(t, logContent, "error")
}
