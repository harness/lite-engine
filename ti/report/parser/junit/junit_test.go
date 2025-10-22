// Copyright 2022 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package junit

import (
	"fmt"
	"io"
	"os"
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

	tests := ParseTests(paths, logrus.New(), envs)
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

	tests := ParseTests(paths, logrus.New(), envs)
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

	tests := ParseTests(paths, logrus.New(), envs)
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

	tests := ParseTests(paths, logrus.New(), envs)
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

	tests := ParseTests(paths, logrus.New(), envs)
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
		getSingleSuiteWithMixedResults(),
		getMultipleSuites(),
		getNestedSuites(),
		getEmptyTestNameShouldBeIgnored(),
		getTestsSkippedByTestIntelligence(),
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

func getSingleSuiteWithMixedResults() struct {
	name           string
	suites         []gojunit.Suite
	expectedCounts TestCounts
} {
	return struct {
		name           string
		suites         []gojunit.Suite
		expectedCounts TestCounts
	}{
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
			Total:       3,
			Passed:      1,
			Failed:      1,
			Skipped:     1,
			SkippedByTi: 0,
			Error:       0,
			Unknown:     0,
		},
	}
}

func getMultipleSuites() struct {
	name           string
	suites         []gojunit.Suite
	expectedCounts TestCounts
} {
	return struct {
		name           string
		suites         []gojunit.Suite
		expectedCounts TestCounts
	}{
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
			Total:       3,
			Passed:      2,
			Failed:      0,
			Skipped:     0,
			SkippedByTi: 0,
			Error:       1,
			Unknown:     0,
		},
	}
}

func getNestedSuites() struct {
	name           string
	suites         []gojunit.Suite
	expectedCounts TestCounts
} {
	return struct {
		name           string
		suites         []gojunit.Suite
		expectedCounts TestCounts
	}{
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
			Total:       2,
			Passed:      1,
			Failed:      1,
			Skipped:     0,
			SkippedByTi: 0,
			Error:       0,
			Unknown:     0,
		},
	}
}

func getEmptyTestNameShouldBeIgnored() struct {
	name           string
	suites         []gojunit.Suite
	expectedCounts TestCounts
} {
	return struct {
		name           string
		suites         []gojunit.Suite
		expectedCounts TestCounts
	}{
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
			Total:       1,
			Passed:      1,
			Failed:      0,
			Skipped:     0,
			SkippedByTi: 0,
			Error:       0,
			Unknown:     0,
		},
	}
}
