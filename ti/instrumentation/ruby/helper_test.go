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
	testGlobs := []string{"spec/**{,/*/**}/*_spec.rb"}
	log := logrus.New()

	// Call the function
	tests := getRubyTestsFromPattern(workspace, testGlobs, log)

	// Assert the results
	assert.Len(t, tests, 0)
	assert.IsType(t, []ti.RunnableTest{}, tests)
}

func TestGetRubyTestsFromPatternPositive(t *testing.T) {
	workspace := "."
	testGlobs := []string{"spec/**{,/*/**}/*_spec.rb"}
	log := logrus.New()

	// Call the function
	tests := getRubyTestsFromPattern(workspace, testGlobs, log)

	// Assert the results
	assert.NotNil(t, tests)
	assert.Len(t, tests, 1)
	assert.IsType(t, []ti.RunnableTest{}, tests)
}
