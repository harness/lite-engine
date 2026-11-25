package docker

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/harness/lite-engine/engine/spec"
	"github.com/moby/moby/api/types/mount"
)

func TestToHostConfigMountsCLIBinaries(t *testing.T) {
	if runtime.GOOS == windowsOS {
		config := toHostConfig(&spec.PipelineConfig{}, &spec.Step{})
		assertCLIMount(t, config.Mounts, `C:\Program Files\lite-engine`, `C:\harness\lite-engine`)
		return
	}

	hcliSourcePath := createTestCLIBinary(t, "hcli")
	harnessSourcePath := createTestCLIBinary(t, "harness")
	originalHcliSourcePath := getHcliSourcePath
	originalHarnessSourcePath := getHarnessSourcePath
	t.Cleanup(func() {
		getHcliSourcePath = originalHcliSourcePath
		getHarnessSourcePath = originalHarnessSourcePath
	})
	getHcliSourcePath = func() string { return hcliSourcePath }
	getHarnessSourcePath = func() string { return harnessSourcePath }

	config := toHostConfig(&spec.PipelineConfig{}, &spec.Step{})
	assertCLIMount(t, config.Mounts, hcliSourcePath, hcliPath)
	assertCLIMount(t, config.Mounts, harnessSourcePath, harnessPath)
}

func TestToHostConfigSkipsMissingHarnessCLI(t *testing.T) {
	if runtime.GOOS == windowsOS {
		t.Skip("Windows mounts the shared CLI directory")
	}

	originalHarnessSourcePath := getHarnessSourcePath
	t.Cleanup(func() { getHarnessSourcePath = originalHarnessSourcePath })
	getHarnessSourcePath = func() string { return filepath.Join(t.TempDir(), "missing-harness") }

	config := toHostConfig(&spec.PipelineConfig{}, &spec.Step{})
	for _, m := range config.Mounts {
		if m.Target == harnessPath {
			t.Fatalf("unexpected harness mount: %#v", m)
		}
	}
}

func createTestCLIBinary(t *testing.T, name string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte("binary"), 0600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	return path
}

func assertCLIMount(t *testing.T, mounts []mount.Mount, source, target string) {
	t.Helper()
	for _, m := range mounts {
		if m.Target == target {
			if m.Type != mount.TypeBind || m.Source != source || !m.ReadOnly {
				t.Fatalf("mount = %#v, want read-only bind from %q to %q", m, source, target)
			}
			return
		}
	}
	t.Fatalf("mount from %q to %q not found in %#v", source, target, mounts)
}
