package instrumentation

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/cespare/xxhash/v2"
	"github.com/harness/lite-engine/internal/filesystem"
	"github.com/sirupsen/logrus"
)

func TestGetNonCodeSentinelPaths(t *testing.T) {
	tests := []struct {
		name          string
		fileChecksums map[string]uint64
		configure     func(*NonCodeConfig)
		want          []string
	}{
		{
			name: "default whitelist includes config and build files",
			fileChecksums: map[string]uint64{
				"pom.xml":                    1,
				"config/service.yaml":        2,
				"README.md":                  3,
				"src/main.java":              4,
				"scripts/setup.sh":           5,
				NonCodeChainPath:             6,
				NonCodeDefaultPath:           1,
				"nested/BUILD.bazel":         7,
				"frontend/package-lock.json": 8,
			},
			want: []string{
				"config/service.yaml",
				"frontend/package-lock.json",
				"nested/BUILD.bazel",
				"pom.xml",
			},
		},
		{
			name: "configured include overrides default whitelist",
			fileChecksums: map[string]uint64{
				"README.md":           1,
				"docs/guide.md":       2,
				"pom.xml":             3,
				"config/service.yaml": 4,
			},
			configure: func(cfg *NonCodeConfig) {
				cfg.Include = []string{"**/*.md"}
			},
			want: []string{"README.md", "docs/guide.md"},
		},
		{
			name: "configured exclude removes included paths",
			fileChecksums: map[string]uint64{
				"config/service.yaml":      1,
				"testdata/service.yaml":    2,
				"nested/testdata/app.yaml": 3,
			},
			configure: func(cfg *NonCodeConfig) {
				cfg.Include = []string{"**/*.yaml"}
				cfg.Exclude = []string{"**/testdata/**"}
			},
			want: []string{"config/service.yaml"},
		},
		{
			name: "reserved sentinel keys are excluded from the path set",
			fileChecksums: map[string]uint64{
				NonCodeChainPath:   6,
				NonCodeDefaultPath: 1,
				"src/main.java":    4,
			},
			want: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var cfg NonCodeConfig
			if tt.configure != nil {
				tt.configure(&cfg)
			}

			got := GetNonCodeSentinelPaths(tt.fileChecksums, cfg)
			if got == nil {
				got = []string{}
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("GetNonCodeSentinelPaths() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestFindNonCodeFilesUsesSentinelPathSelection(t *testing.T) {
	fileChecksums := map[string]uint64{
		"pom.xml":             1,
		"config/service.yaml": 2,
		"src/main.java":       3,
		NonCodeDefaultPath:    1,
	}

	got := FindNonCodeFiles(fileChecksums, NonCodeConfig{})
	want := "config/service.yaml#pom.xml"

	if got != want {
		t.Fatalf("FindNonCodeFiles() = %q, want %q", got, want)
	}
}

func TestFindNonCodeFilesEmptySetExcludesDefaultPath(t *testing.T) {
	fileChecksums := map[string]uint64{
		"src/main.java":    3,
		NonCodeDefaultPath: 1,
	}

	got := FindNonCodeFiles(fileChecksums, NonCodeConfig{})
	if got != "" {
		t.Fatalf("FindNonCodeFiles() = %q, want empty string", got)
	}
}

func TestPopulateNonCodeEntitiesUsesStaticSentinelPaths(t *testing.T) {
	fileChecksums := map[string]uint64{
		"pom.xml":               1,
		"config/service.yaml":   2,
		"src/main.java":         3,
		NonCodeChainPath:        4,
		NonCodeDefaultPath:      1,
		"docs/readme.md":        5,
		"testdata/fixture.yaml": 6,
		"generated/output.yaml": 7,
	}
	nonCodeConfig := NonCodeConfig{
		Include: []string{"**/*.yaml", "**/pom.xml"},
		Exclude: []string{"**/generated/**", "**/testdata/**"},
	}

	test, chain := PopulateNonCodeEntities(fileChecksums, nonCodeConfig)
	got := test.IndicativeChains[0].SourcePaths
	want := []string{"config/service.yaml", "pom.xml"}

	if test.Path != NonCodeChainPath {
		t.Fatalf("test.Path = %q, want %q", test.Path, NonCodeChainPath)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("source paths = %#v, want %#v", got, want)
	}
	if chain.Path != NonCodeChainPath {
		t.Fatalf("chain.Path = %q, want %q", chain.Path, NonCodeChainPath)
	}
	if chain.TestChecksum != "4" {
		t.Fatalf("chain.TestChecksum = %q, want %q", chain.TestChecksum, "4")
	}
	if chain.Checksum == "" || chain.Checksum == "0" {
		t.Fatalf("chain.Checksum = %q, want a non-zero checksum", chain.Checksum)
	}
}

func TestPopulateNonCodeEntitiesUsesDefaultSourceForEmptySet(t *testing.T) {
	fileChecksums := map[string]uint64{
		"src/main.java":    3,
		NonCodeChainPath:   4,
		NonCodeDefaultPath: 1,
	}

	test, chain := PopulateNonCodeEntities(fileChecksums, NonCodeConfig{})

	got := test.IndicativeChains[0].SourcePaths
	want := []string{NonCodeDefaultPath}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("source paths = %#v, want %#v", got, want)
	}
	if chain.Checksum == "0" {
		t.Fatal("empty non-code set must use a non-zero default-source chain checksum")
	}
}

func TestGetGitFileChecksumsAddsDefaultNonCodeFile(t *testing.T) {
	repoDir := t.TempDir()
	runGit := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = repoDir
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v failed: %v\n%s", args, err, output)
		}
	}

	runGit("init")
	if err := os.WriteFile(filepath.Join(repoDir, "main.go"), []byte("package main\n"), 0644); err != nil {
		t.Fatalf("failed to write source file: %v", err)
	}
	runGit("add", "main.go")
	runGit("-c", "user.email=test@example.com", "-c", "user.name=Test User", "commit", "-m", "initial")

	checksums, _, err := GetGitFileChecksums(context.Background(), repoDir, logrus.New())
	if err != nil {
		t.Fatalf("GetGitFileChecksums() unexpected error: %v", err)
	}
	if got := checksums[NonCodeDefaultPath]; got != constantChecksum {
		t.Fatalf("default non-code checksum = %d, want %d", got, constantChecksum)
	}
	if got := checksums[NonCodeChainPath]; got != xxhash.Sum64String("") {
		t.Fatalf("empty non-code sentinel checksum = %d, want %d", got, xxhash.Sum64String(""))
	}
}

func TestGetParsedTiConfigParsesNonCodeConfig(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, tiConfigPath)
	config := []byte(`config:
  ignore:
    - "**/*.tmp"
  nonCode:
    include:
      - "**/*.md"
    exclude:
      - "**/generated/**"
`)
	if err := os.WriteFile(configPath, config, 0644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	got, err := getParsedTiConfig(tmpDir, filesystem.New())
	if err != nil {
		t.Fatalf("getParsedTiConfig() unexpected error: %v", err)
	}

	if !reflect.DeepEqual(got.Config.Ignore, []string{"**/*.tmp"}) {
		t.Fatalf("ignore = %#v", got.Config.Ignore)
	}
	if !reflect.DeepEqual(got.Config.NonCode.Include, []string{"**/*.md"}) {
		t.Fatalf("nonCode.include = %#v", got.Config.NonCode.Include)
	}
	if !reflect.DeepEqual(got.Config.NonCode.Exclude, []string{"**/generated/**"}) {
		t.Fatalf("nonCode.exclude = %#v", got.Config.NonCode.Exclude)
	}
}
