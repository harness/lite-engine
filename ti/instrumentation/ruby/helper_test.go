package ruby

import (
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
