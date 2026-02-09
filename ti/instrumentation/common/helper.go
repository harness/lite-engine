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
	defaultDirPerm           = 0755
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

	if err := os.MkdirAll(destDir, defaultDirPerm); err != nil {
		return fmt.Errorf("failed to create destination directory: %w", err)
	}

	fsys, err := archives.FileSystem(ctx, archivePath, nil)
	if err != nil {
		return fmt.Errorf("failed to open archive: %w", err)
	}

	allPaths, err := collectArchivePaths(fsys)
	if err != nil {
		return err
	}

	destDir, err = adjustDestDirIfNeeded(destDir, archivePath, allPaths)
	if err != nil {
		return err
	}

	return extractArchiveFiles(fsys, destDir)
}

// collectArchivePaths collects all paths from the archive filesystem.
func collectArchivePaths(fsys fs.FS) ([]string, error) {
	var allPaths []string
	err := fs.WalkDir(fsys, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path != "." {
			allPaths = append(allPaths, path)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to read archive contents: %w", err)
	}
	return allPaths, nil
}

// adjustDestDirIfNeeded creates a subfolder if files don't share a common root.
func adjustDestDirIfNeeded(destDir, archivePath string, allPaths []string) (string, error) {
	if multipleTopLevels(allPaths) {
		destDir = filepath.Join(destDir, folderNameFromFileName(archivePath))
		if err := os.MkdirAll(destDir, defaultDirPerm); err != nil {
			return "", fmt.Errorf("failed to create implicit top-level folder: %w", err)
		}
	}
	return destDir, nil
}

// extractArchiveFiles walks through the archive and extracts each file.
func extractArchiveFiles(fsys fs.FS, destDir string) error {
	return fs.WalkDir(fsys, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || path == "." {
			return nil
		}

		targetPath := filepath.Join(destDir, path)
		if !isPathSafe(destDir, targetPath) {
			return nil
		}

		if d.IsDir() {
			return extractDirectory(targetPath)
		}

		return extractFile(fsys, path, targetPath, d)
	})
}

// isPathSafe validates that the target path doesn't escape the destination directory.
func isPathSafe(destDir, targetPath string) bool {
	rel, err := filepath.Rel(destDir, targetPath)
	return err == nil && !strings.HasPrefix(rel, "..")
}

// extractDirectory creates a directory at the target path.
func extractDirectory(targetPath string) error {
	if err := os.MkdirAll(targetPath, defaultDirPerm); err != nil {
		return nil
	}
	return nil
}

// extractFile extracts a single file from the archive to the target path.
func extractFile(fsys fs.FS, path, targetPath string, d fs.DirEntry) error {
	if err := os.MkdirAll(filepath.Dir(targetPath), defaultDirPerm); err != nil {
		return nil
	}

	info, err := d.Info()
	if err != nil {
		return nil
	}

	srcFile, err := fsys.Open(path)
	if err != nil {
		return nil
	}
	defer srcFile.Close()

	dstFile, err := os.OpenFile(targetPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, info.Mode())
	if err != nil {
		return nil
	}
	defer dstFile.Close()

	if _, err := io.Copy(dstFile, srcFile); err != nil {
		os.Remove(targetPath)
		return nil
	}

	return nil
}

// multipleTopLevels checks if files have multiple top-level directories
func multipleTopLevels(files []string) bool {
	if len(files) == 0 {
		return false
	}

	var topLevel string
	for _, f := range files {
		parts := strings.Split(filepath.ToSlash(f), "/")
		if len(parts) == 0 {
			continue
		}
		currentTop := parts[0]

		if topLevel == "" {
			topLevel = currentTop
		} else if topLevel != currentTop {
			return true
		}
	}
	return false
}

// folderNameFromFileName extracts a folder name from the archive filename
func folderNameFromFileName(archivePath string) string {
	base := filepath.Base(archivePath)
	for {
		ext := filepath.Ext(base)
		if ext == "" {
			break
		}
		base = strings.TrimSuffix(base, ext)
	}
	return base
}
