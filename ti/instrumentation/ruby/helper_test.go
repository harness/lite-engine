package ruby

import (
	"reflect"
	"testing"

	ti "github.com/harness/ti-client/types"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

func TestGetRubyTestsFromPatternNegative(t *testing.T) {
	// Mock input values
	workspace := "/path/to/workspace"
	testGlobs := []string{"spec/**/*_spec.rb"}
	filterExcludeGlobs := []string{"**/vendor/**/*.rb"}
	log := logrus.New()

	// Call the function
	tests := getRubyTestsFromPattern(workspace, testGlobs, filterExcludeGlobs, log)

	// Assert the results
	assert.Len(t, tests, 0)
	assert.IsType(t, []ti.RunnableTest{}, tests)
}

func TestGetRubyTestsFromPatternPositive(t *testing.T) {
	workspace := "."
	testGlobs := []string{"spec/**/*_spec.rb"}
	filterExcludeGlobs := []string{"**/vendor/**/*.rb"}
	log := logrus.New()

	// Call the function
	tests := getRubyTestsFromPattern(workspace, testGlobs, filterExcludeGlobs, log)

	// Assert the results
	assert.NotNil(t, tests)
	assert.Len(t, tests, 2)
	assert.IsType(t, []ti.RunnableTest{}, tests)
}

func TestGetRubyTestsFromPatternPositiveNoVendorIgnore(t *testing.T) {
	workspace := "."
	testGlobs := []string{"spec/**/*_spec.rb"}
	filterExcludeGlobs := []string{}
	log := logrus.New()

	// Call the function
	tests := getRubyTestsFromPattern(workspace, testGlobs, filterExcludeGlobs, log)

	// Assert the results
	assert.NotNil(t, tests)
	assert.Len(t, tests, 3)
	assert.IsType(t, []ti.RunnableTest{}, tests)
}

func TestGetRubyGlobs(t *testing.T) {
	// Test case 1: testGlobs is empty, envs does not contain "TI_SKIP_EXCLUDE_VENDOR"
	testGlobs := []string{}
	envs := map[string]string{}
	includeGlobs, excludeGlobs := GetRubyGlobs(testGlobs, envs)
	expectedIncludeGlobs := defaultTestGlobs
	expectedExcludeGlobs := filterExcludeGlobs
	if !reflect.DeepEqual(includeGlobs, expectedIncludeGlobs) || !reflect.DeepEqual(excludeGlobs, expectedExcludeGlobs) {
		t.Errorf("Test case 1 failed. Expected include globs: %v, got: %v. Expected exclude globs: %v, got: %v", expectedIncludeGlobs, includeGlobs, expectedExcludeGlobs, excludeGlobs)
	}

	// Test case 2: testGlobs is not empty, envs does not contain "TI_SKIP_EXCLUDE_VENDOR"
	testGlobs = []string{"test1.rb", "test2.rb"}
	includeGlobs, excludeGlobs = GetRubyGlobs(testGlobs, envs)
	expectedIncludeGlobs = testGlobs
	expectedExcludeGlobs = filterExcludeGlobs
	if !reflect.DeepEqual(includeGlobs, expectedIncludeGlobs) || !reflect.DeepEqual(excludeGlobs, expectedExcludeGlobs) {
		t.Errorf("Test case 2 failed. Expected include globs: %v, got: %v. Expected exclude globs: %v, got: %v", expectedIncludeGlobs, includeGlobs, expectedExcludeGlobs, excludeGlobs)
	}

	// Test case 3: testGlobs is empty, envs contains "TI_SKIP_EXCLUDE_VENDOR" set to "true"
	testGlobs = []string{}
	envs["TI_SKIP_EXCLUDE_VENDOR"] = "true"
	includeGlobs, excludeGlobs = GetRubyGlobs(testGlobs, envs)
	expectedIncludeGlobs = defaultTestGlobs
	expectedExcludeGlobs = []string{}
	if !reflect.DeepEqual(includeGlobs, expectedIncludeGlobs) || !reflect.DeepEqual(excludeGlobs, expectedExcludeGlobs) {
		t.Errorf("Test case 3 failed. Expected include globs: %v, got: %v. Expected exclude globs: %v, got: %v", expectedIncludeGlobs, includeGlobs, expectedExcludeGlobs, excludeGlobs)
	}

	// Test case 4: testGlobs is not empty, envs contains "TI_SKIP_EXCLUDE_VENDOR" set to "true"
	testGlobs = []string{"test1.rb", "test2.rb"}
	includeGlobs, excludeGlobs = GetRubyGlobs(testGlobs, envs)
	expectedIncludeGlobs = testGlobs
	expectedExcludeGlobs = []string{}
	if !reflect.DeepEqual(includeGlobs, expectedIncludeGlobs) || !reflect.DeepEqual(excludeGlobs, expectedExcludeGlobs) {
		t.Errorf("Test case 4 failed. Expected include globs: %v, got: %v. Expected exclude globs: %v, got: %v", expectedIncludeGlobs, includeGlobs, expectedExcludeGlobs, excludeGlobs)
	}
}
