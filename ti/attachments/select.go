// Copyright 2026 Harness Inc. All rights reserved.
// Use of this source code is governed by the PolyForm Free Trial 1.0.0 license
// that can be found in the licenses directory at the root of this repository, also available at
// https://polyformproject.org/wp-content/uploads/2020/05/PolyForm-Free-Trial-1.0.0.txt.

package attachments

import (
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/harness/ti-client/types"
)

const (
	bytesPerMiB = 1 << 20
	bytesPerGiB = 1 << 30
	// maxFileBytes is the largest single file that is uploaded.
	maxFileBytes = int64(5 * bytesPerGiB)
)

var (
	attachmentPattern = regexp.MustCompile(`\[\[ATTACHMENT\|([^\]\r\n]+)\]\]`)
	retryDirPattern   = regexp.MustCompile(`-retry(\d+)$`)
)

// testCase is one <testcase> of a JUnit report with the attachment paths found in its <system-out>.
type testCase struct {
	failed      bool
	attachments []string
}

// file is a file to upload. The destination is relative to the step upload location and starts with the
// test result directory's path relative to the project root.
type file struct {
	sourcePath  string
	destination string
	size        int64
}

// selectFiles returns the files to upload from the result directories of the selected tests, and messages
// for everything that was skipped. With all set every test with attachments is selected, otherwise only
// failed tests and tests that ran in a retry directory.
func selectFiles(root string, junitFiles []string, all bool) (files []file, skipped []string) {
	root = filepath.Clean(root)
	dirs, skipped := chooseDirs(root, junitFiles, all)

	var walked []string
	for _, dir := range dirs {
		if isWithinAny(walked, dir) {
			continue
		}
		walked = append(walked, dir)
		dirFiles, oversized, err := collectDirFiles(root, dir)
		if err != nil {
			skipped = append(skipped, fmt.Sprintf("skipping %s: %v", dir, err))
			continue
		}
		for _, f := range oversized {
			skipped = append(skipped, fmt.Sprintf("skipping %s: file is over the %d GiB per-file limit",
				f, maxFileBytes/bytesPerGiB))
		}
		files = append(files, dirFiles...)
	}
	return files, skipped
}

// chooseDirs returns the sorted, distinct result directories of the selected tests.
func chooseDirs(root string, junitFiles []string, all bool) (dirs, skipped []string) {
	chosen := map[string]bool{}
	for _, junit := range junitFiles {
		cases, err := parseCases(junit)
		if err != nil {
			continue
		}
		for _, tc := range cases {
			caseDirs, retry, msgs := resultDirs(root, junit, tc.attachments)
			skipped = append(skipped, msgs...)
			if len(caseDirs) == 0 || (!all && !tc.failed && !retry) {
				continue
			}
			for _, d := range caseDirs {
				chosen[d] = true
			}
		}
	}
	for d := range chosen {
		dirs = append(dirs, d)
	}
	sort.Strings(dirs)
	return dirs, skipped
}

// resultDirs maps the attachment paths of one test to their result directories and reports whether any of
// them is a retry directory. Files that do not exist or are outside root are ignored.
func resultDirs(root, junit string, attachments []string) (dirs []string, retry bool, skipped []string) {
	seen := map[string]bool{}
	for _, rel := range attachments {
		file, dir, ok := resolveAttachment(root, junit, rel)
		if !ok {
			continue
		}
		if dir == root {
			skipped = append(skipped, fmt.Sprintf("ignoring %s: attachments directly in the project root are not uploaded", file))
			continue
		}
		if !seen[dir] {
			seen[dir] = true
			dirs = append(dirs, dir)
		}
		retry = retry || isRetryDir(root, dir)
	}
	return dirs, retry, skipped
}

// parseCases returns one entry per <testcase> with the attachment paths from its <system-out> and whether
// it has a <failure> or <error> element.
func parseCases(path string) ([]testCase, error) { //nolint:gocyclo // one flat XML token switch
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	decoder := xml.NewDecoder(f)
	var cases []testCase
	var current *testCase
	var out strings.Builder
	inOut := false
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		switch el := token.(type) {
		case xml.StartElement:
			switch el.Name.Local {
			case "testcase":
				current = &testCase{}
			case "failure", "error":
				if current != nil {
					current.failed = true
				}
			case "system-out":
				if current != nil {
					inOut = true
					out.Reset()
				}
			}
		case xml.CharData:
			if inOut {
				out.Write(el)
			}
		case xml.EndElement:
			switch el.Name.Local {
			case "system-out":
				if inOut && current != nil {
					for _, m := range attachmentPattern.FindAllStringSubmatch(out.String(), -1) {
						current.attachments = append(current.attachments, strings.TrimSpace(m[1]))
					}
				}
				inOut = false
			case "testcase":
				if current != nil {
					cases = append(cases, *current)
				}
				current = nil
			}
		}
	}
	return cases, nil
}

// resolveAttachment resolves an [[ATTACHMENT|path]] line of junit to the file and its result directory, which is
// root itself for a file directly in root. ok is false unless the path names a regular file inside root.
func resolveAttachment(root, junit, attachment string) (file, dir string, ok bool) {
	file = resolveAbs(filepath.Dir(junit), attachment)
	if !isRegularFile(file) || !isWithin(root, file) {
		return file, "", false
	}
	return file, attachmentResultDir(root, filepath.Dir(file)), true
}

// testArtifactType names the artifact types the step uploads and the test report links: the Playwright trace and the
// video. Other files get "" and are not uploaded, to save storage: the trace already holds the screenshots of the
// test. To upload more kinds of files, give them an artifact type here.
func testArtifactType(dest string) string {
	name := strings.ToLower(path.Base(dest))
	switch {
	case name == "trace.zip":
		return types.TestArtifactTypeTrace
	case strings.HasSuffix(name, ".webm"):
		return types.TestArtifactTypeVideo
	default:
		return ""
	}
}

// retryAttempt returns N for a file in a <test>-retryN directory, or below one, and 0 for the first run. The nearest
// such directory counts.
func retryAttempt(dest string) int {
	for dir := path.Dir(dest); dir != "." && dir != "/"; dir = path.Dir(dir) {
		if m := retryDirPattern.FindStringSubmatch(path.Base(dir)); m != nil {
			n, _ := strconv.Atoi(m[1])
			return n
		}
	}
	return 0
}

// resolveAbs makes p absolute and clean, relative to base. A path that starts with a separator but has no
// drive letter, such as /harness/app on Windows, takes the drive of base instead of being joined to it.
func resolveAbs(base, p string) string {
	p = filepath.FromSlash(p)
	if filepath.VolumeName(p) == "" && p != "" && os.IsPathSeparator(p[0]) {
		return filepath.Clean(filepath.VolumeName(base) + p)
	}
	if filepath.IsAbs(p) {
		return filepath.Clean(p)
	}
	if abs, err := filepath.Abs(filepath.Join(base, p)); err == nil {
		return abs
	}
	return filepath.Join(base, p)
}

// isWithinAny reports whether path is strictly inside any of the dirs.
func isWithinAny(dirs []string, path string) bool {
	for _, d := range dirs {
		if isWithin(d, path) {
			return true
		}
	}
	return false
}

func isRegularFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

// isWithin reports whether path is strictly inside root.
func isWithin(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == "." {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// attachmentResultDir maps a testInfo.attach directory (<test-dir>/attachments) to its test result directory
// so the destination keeps the test name.
func attachmentResultDir(root, dir string) string {
	if filepath.Base(dir) == "attachments" {
		if parent := filepath.Dir(dir); isWithin(root, parent) {
			return parent
		}
	}
	return filepath.Clean(dir)
}

func isRetryDir(root, dir string) bool {
	for d := dir; isWithin(root, d); d = filepath.Dir(d) {
		if retryDirPattern.MatchString(filepath.Base(d)) {
			return true
		}
	}
	return false
}

// collectDirFiles returns the regular files under dir that are uploaded (see testArtifactType). Destinations are
// relative to the project root, not just the directory name, so result directories that share a name (for example
// in different test projects) cannot collide. Files over the per-file limit are returned in oversized instead, so
// one large file does not drop the rest of the directory.
func collectDirFiles(root, dir string) (files []file, oversized []string, err error) {
	prefix, err := filepath.Rel(root, dir)
	if err != nil {
		return nil, nil, err
	}
	prefix = filepath.ToSlash(prefix)
	err = filepath.WalkDir(dir, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() || testArtifactType(entry.Name()) == "" {
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		if info.Size() > maxFileBytes {
			oversized = append(oversized, path)
			return nil
		}
		files = append(files, file{
			sourcePath: path, destination: prefix + "/" + filepath.ToSlash(rel), size: info.Size(),
		})
		return nil
	})
	return files, oversized, err
}
