package common

import (
	"path/filepath"

	ti "github.com/harness/ti-client/types"
	"github.com/mattn/go-zglob"
)

const (
	HarnessDefaultReportPath = "harness_test_results.xml"
)

type NodeType int32

const (
	NodeType_SOURCE   NodeType = 0 //nolint:revive,stylecheck
	NodeType_TEST     NodeType = 1 //nolint:revive,stylecheck
	NodeType_CONF     NodeType = 2 //nolint:revive,stylecheck
	NodeType_RESOURCE NodeType = 3 //nolint:revive,stylecheck
	NodeType_OTHER    NodeType = 4 //nolint:revive,stylecheck
)

type LangType int32

const (
	LangType_JAVA    LangType = 0 //nolint:revive,stylecheck
	LangType_CSHARP  LangType = 1 //nolint:revive,stylecheck
	LangType_PYTHON  LangType = 2 //nolint:revive,stylecheck
	LangType_UNKNOWN LangType = 3 //nolint:revive,stylecheck
)

// Node holds data about a source code
type Node struct {
	Pkg    string
	Class  string
	Method string
	File   string
	Lang   LangType
	Type   NodeType
}

// RunnerArgs to add additinal args for runner
type RunnerArgs struct {
	ModuleList []string
}

// GetFiles gets list of all file paths matching a provided regex
func GetFiles(path string) ([]string, error) {
	matches, err := zglob.Glob(path)
	if err != nil {
		return []string{}, err
	}
	return matches, err
}

// GetUniqueTestStrings extract list of test strings from Class
// It should only work if Class is the only primary identifier of the test selection
func GetUniqueTestStrings(tests []ti.RunnableTest) []string {
	// Use only unique class
	set := make(map[ti.RunnableTest]interface{})
	ut := []string{}
	for _, t := range tests {
		w := ti.RunnableTest{Class: t.Class}
		if _, ok := set[w]; ok {
			// The test has already been added
			continue
		}
		set[w] = struct{}{}
		ut = append(ut, t.Class)
	}
	return ut
}

// SimpleAutoDetectTestFiles discovers test files using globs and returns relative paths from workspace
func SimpleAutoDetectTestFiles(workspace string, testGlobs []string) ([]string, error) {
	// Simple default test patterns if none provided
	defaultGlobs := []string{
		"**/*_test.py",
		"**/test_*.py",
		"**/spec/**/*_spec.rb",
		"**/*Test.java",
		"**/*Tests.java",
		"**/*Test.cs",
		"**/*Tests.cs",
	}

	// Use provided globs or defaults
	globsToUse := testGlobs
	if len(globsToUse) == 0 {
		globsToUse = defaultGlobs
	}

	allFiles := make([]string, 0)

	// Search for each glob pattern under workspace
	for _, glob := range globsToUse {
		// Create full pattern: workspace + glob
		fullPattern := filepath.Join(workspace, glob)

		// Find matching files
		matches, err := zglob.Glob(fullPattern)
		if err != nil {
			continue // Skip failed patterns
		}

		// Convert to relative paths from workspace
		for _, match := range matches {
			if relPath, err := filepath.Rel(workspace, match); err == nil {
				allFiles = append(allFiles, relPath)
			}
		}
	}

	// Remove duplicates
	uniqueFiles := make([]string, 0)
	seen := make(map[string]bool)
	for _, file := range allFiles {
		if !seen[file] {
			uniqueFiles = append(uniqueFiles, file)
			seen[file] = true
		}
	}

	return uniqueFiles, nil
}
