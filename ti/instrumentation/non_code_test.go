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
		cmd := exec.CommandContext(context.Background(), "git", args...)
		cmd.Dir = repoDir
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v failed: %v\n%s", args, err, output)
		}
	}

	runGit("init")
	if err := os.WriteFile(filepath.Join(repoDir, "main.go"), []byte("package main\n"), 0600); err != nil {
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
	if err := os.WriteFile(configPath, config, 0600); err != nil {
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

func TestApplyChangedFilesChecksum(t *testing.T) {
	tests := []struct {
		name    string
		in      map[string]uint64
		changed []string
		ignore  []string
		want    map[string]uint64
	}{
		{
			name:    "changed files get the fixed checksum, others keep the real one",
			in:      map[string]uint64{"a.java": 100, "b.java": 200, "c.java": 300},
			changed: []string{"a.java", "c.java"},
			want:    map[string]uint64{"a.java": changedFileChecksum, "b.java": 200, "c.java": changedFileChecksum},
		},
		{
			name:    "nil changed files is a no-op",
			in:      map[string]uint64{"a.java": 100},
			changed: nil,
			want:    map[string]uint64{"a.java": 100},
		},
		{
			name:    "deleted file (absent from tree) is not added",
			in:      map[string]uint64{"a.java": 100},
			changed: []string{"deleted.java"},
			want:    map[string]uint64{"a.java": 100},
		},
		{
			name:    "ignored changed file keeps the ignore checksum",
			in:      map[string]uint64{"gen/x.java": constantChecksum, "a.java": 100},
			changed: []string{"gen/x.java", "a.java"},
			ignore:  []string{"gen/**"},
			want:    map[string]uint64{"gen/x.java": constantChecksum, "a.java": changedFileChecksum},
		},
		{
			name:    "duplicates are harmless",
			in:      map[string]uint64{"a.java": 100},
			changed: []string{"a.java", "a.java"},
			want:    map[string]uint64{"a.java": changedFileChecksum},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			applyChangedFilesChecksum(tt.in, tt.changed, tt.ignore)
			if !reflect.DeepEqual(tt.in, tt.want) {
				t.Errorf("applyChangedFilesChecksum() = %v, want %v", tt.in, tt.want)
			}
		})
	}
}

func TestChangedFileChecksumIsDistinctFromConstants(t *testing.T) {
	if changedFileChecksum == constantChecksum {
		t.Errorf("changedFileChecksum %d must differ from the ignore checksum", changedFileChecksum)
	}
}

func TestChangedNonCodeFileChangesNonCodeChain(t *testing.T) {
	chainFor := func(changed []string, cfg NonCodeConfig) string {
		fc := map[string]uint64{"src/a.java": 10, "pom.xml": 20, "README.md": 30, "config/app.yaml": 40}
		applyChangedFilesChecksum(fc, changed, nil)
		fc[NonCodeDefaultPath] = constantChecksum
		fc[NonCodeChainPath] = xxhash.Sum64String(FindNonCodeFiles(fc, cfg))
		_, chain := PopulateNonCodeEntities(fc, cfg)
		return chain.Checksum
	}
	real := chainFor(nil, NonCodeConfig{})
	if chainFor([]string{"pom.xml"}, NonCodeConfig{}) == real {
		t.Error("changed included non-code file must change the non-code chain")
	}
	if chainFor([]string{"README.md"}, NonCodeConfig{}) != real {
		t.Error("changed non-code file outside the include set must not change the chain")
	}
	if chainFor([]string{"src/a.java"}, NonCodeConfig{}) != real {
		t.Error("changed code file must not change the non-code chain")
	}
	excl := NonCodeConfig{}
	excl.Exclude = []string{"config/**"}
	if chainFor([]string{"config/app.yaml"}, excl) != chainFor(nil, excl) {
		t.Error("changed excluded non-code file must not change the chain")
	}
}

func TestGetGitFileChecksumsWithChangedFiles(t *testing.T) {
	repoDir := t.TempDir()
	runGit := func(args ...string) {
		t.Helper()
		cmd := exec.CommandContext(context.Background(), "git", args...)
		cmd.Dir = repoDir
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v failed: %v\n%s", args, err, output)
		}
	}
	write := func(name, content string) {
		p := filepath.Join(repoDir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("src/A.java", "class A {}")
	write("src/B.java", "class B {}")
	write("gen/G.java", "class G {}")
	write("pom.xml", "<project/>")
	write(tiConfigPath, "config:\n  ignore:\n    - gen/**\n")
	runGit("init")
	runGit("add", ".")
	runGit("-c", "user.email=t@t", "-c", "user.name=t", "commit", "-m", "init")

	ctx := context.Background()
	real, _, err := GetGitFileChecksums(ctx, repoDir, logrus.New())
	if err != nil {
		t.Fatal(err)
	}
	withNil, _, err := GetGitFileChecksumsWithChangedFiles(ctx, repoDir, logrus.New(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(real, withNil) {
		t.Error("no changed files must equal GetGitFileChecksums")
	}

	got, _, err := GetGitFileChecksumsWithChangedFiles(ctx, repoDir, logrus.New(), []string{"src/A.java", "gen/G.java", "pom.xml", "gone.java"})
	if err != nil {
		t.Fatal(err)
	}
	if got["src/A.java"] != changedFileChecksum || got["src/A.java"] == real["src/A.java"] {
		t.Errorf("changed file checksum = %d", got["src/A.java"])
	}
	if got["src/B.java"] != real["src/B.java"] {
		t.Error("unchanged file must keep its real checksum")
	}
	if got["gen/G.java"] != constantChecksum {
		t.Errorf("ignored file checksum = %d, want %d", got["gen/G.java"], constantChecksum)
	}
	if got["pom.xml"] != changedFileChecksum {
		t.Errorf("changed non-code file checksum = %d", got["pom.xml"])
	}
	if _, ok := got["gone.java"]; ok {
		t.Error("deleted file must not be added")
	}
	again, _, _ := GetGitFileChecksums(ctx, repoDir, logrus.New())
	if !reflect.DeepEqual(real, again) {
		t.Error("GetGitFileChecksums must keep returning real checksums (used for upload)")
	}
}
