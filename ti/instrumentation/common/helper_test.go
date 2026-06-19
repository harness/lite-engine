package common

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"
)

func createTestZip(t *testing.T, dir string, files map[string]string) string {
	t.Helper()
	zipPath := filepath.Join(dir, "test.zip")
	f, err := os.Create(zipPath)
	if err != nil {
		t.Fatalf("create zip: %v", err)
	}
	w := zip.NewWriter(f)
	for name, content := range files {
		fw, err := w.Create(name)
		if err != nil {
			t.Fatalf("add file to zip: %v", err)
		}
		if _, err := fw.Write([]byte(content)); err != nil {
			t.Fatalf("write file content: %v", err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close zip writer: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close file: %v", err)
	}
	return zipPath
}

func TestExtractArchive(t *testing.T) {
	tmpDir := t.TempDir()
	zipPath := createTestZip(t, tmpDir, map[string]string{
		"a.txt":       "hello",
		"sub/b.txt":   "world",
		"sub/c/d.txt": "nested",
	})

	destDir := filepath.Join(tmpDir, "out")
	if err := ExtractArchive(zipPath, destDir); err != nil {
		t.Fatalf("ExtractArchive: %v", err)
	}

	for _, tc := range []struct {
		path    string
		content string
	}{
		{"a.txt", "hello"},
		{filepath.Join("sub", "b.txt"), "world"},
		{filepath.Join("sub", "c", "d.txt"), "nested"},
	} {
		data, err := os.ReadFile(filepath.Join(destDir, tc.path))
		if err != nil {
			t.Errorf("read %s: %v", tc.path, err)
			continue
		}
		if string(data) != tc.content {
			t.Errorf("%s: got %q, want %q", tc.path, data, tc.content)
		}
	}
}

func TestExtractArchive_NonexistentArchive(t *testing.T) {
	tmpDir := t.TempDir()

	err := ExtractArchive(filepath.Join(tmpDir, "nonexistent.zip"), filepath.Join(tmpDir, "out"))
	if err == nil {
		t.Fatal("expected error for nonexistent archive, got nil")
	}
}

func TestIsPathSafe(t *testing.T) {
	const destDir = "/dest"
	tests := []struct {
		name     string
		destDir  string
		target   string
		expected bool
	}{
		{"safe path", destDir, destDir + "/file.txt", true},
		{"traversal", destDir, destDir + "/../etc/passwd", false},
		{"exact dest", destDir, destDir, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := isPathSafe(tc.destDir, tc.target); got != tc.expected {
				t.Errorf("isPathSafe(%q, %q) = %v, want %v", tc.destDir, tc.target, got, tc.expected)
			}
		})
	}
}

func TestFolderNameFromFileName(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"archive.tar.gz", "archive"},
		{"file.zip", "file"},
		{"noext", "noext"},
		{"/some/path/agent.tar.gz", "agent"},
	}
	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			if got := folderNameFromFileName(tc.input); got != tc.want {
				t.Errorf("folderNameFromFileName(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}
