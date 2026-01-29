package common

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	ti "github.com/harness/ti-client/types"
	"github.com/mattn/go-zglob"
	"github.com/mholt/archives"
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

// ExtractArchive extracts an archive to the destination directory using mholt/archives.
// It includes path traversal protection by validating extracted file paths.
func ExtractArchive(archivePath, destDir string) error {
	ctx := context.Background()

	fsys, err := archives.FileSystem(ctx, archivePath, nil)
	if err != nil {
		return fmt.Errorf("failed to open archive: %w", err)
	}

	return fs.WalkDir(fsys, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if path == "." {
			return nil
		}

		targetPath := filepath.Join(destDir, path)
		if !strings.HasPrefix(filepath.Clean(targetPath), filepath.Clean(destDir)+string(os.PathSeparator)) {
			return fmt.Errorf("invalid file path: %s", path)
		}

		if d.IsDir() {
			return os.MkdirAll(targetPath, 0755)
		}

		if err := os.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
			return fmt.Errorf("failed to create directory: %w", err)
		}

		srcFile, err := fsys.Open(path)
		if err != nil {
			return fmt.Errorf("failed to open file in archive: %w", err)
		}
		defer srcFile.Close()

		dstFile, err := os.Create(targetPath)
		if err != nil {
			return fmt.Errorf("failed to create file: %w", err)
		}
		defer dstFile.Close()

		if _, err := io.Copy(dstFile, srcFile); err != nil {
			return fmt.Errorf("failed to copy file contents: %w", err)
		}

		return nil
	})
}
