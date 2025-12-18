package pipeline

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGetSharedVolPath(t *testing.T) {
	// Save and restore original env
	origWorkdir := os.Getenv("HARNESS_WORKDIR")
	defer os.Setenv("HARNESS_WORKDIR", origWorkdir)

	t.Run("no workdir returns default", func(t *testing.T) {
		os.Setenv("HARNESS_WORKDIR", "")
		assert.Equal(t, defaultSharedVolPath, GetSharedVolPath())
	})

	t.Run("workdir set returns workdir/engine", func(t *testing.T) {
		os.Setenv("HARNESS_WORKDIR", "/my/workdir")
		expected := filepath.Join("/my/workdir", "tmp/engine")
		assert.Equal(t, expected, GetSharedVolPath())
	})

	t.Run("windows style workdir", func(t *testing.T) {
		os.Setenv("HARNESS_WORKDIR", "D:\\runner-workspace")
		expected := filepath.Join("D:\\runner-workspace", "tmp", "engine")
		assert.Equal(t, expected, GetSharedVolPath())
	})
}
