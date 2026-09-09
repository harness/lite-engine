package callgraph

import (
	"reflect"
	"testing"

	"github.com/harness/lite-engine/ti/config"
	"github.com/harness/lite-engine/ti/instrumentation"
	"github.com/sirupsen/logrus"
)

func TestCreateUploadPayloadUsesStaticNonCodeSentinelPaths(t *testing.T) {
	fileChecksums := map[string]uint64{
		"pom.xml":                        1,
		"config/service.yaml":            2,
		"src/main/java/AppTest.java":     3,
		instrumentation.NonCodeChainPath: 4,
		"README.md":                      5,
	}
	nonCodeConfig := instrumentation.NonCodeConfig{
		Include: []string{"**/*.yaml", "**/pom.xml"},
	}
	cfg := config.New("", "", "acct", "org", "proj", "", "", "", "", "", "", "", "", "", "", "", false, "", "")

	payload, err := CreateUploadPayload(
		nil,
		fileChecksums,
		nonCodeConfig,
		"repo",
		&cfg,
		"sha",
		nil,
		logrus.New(),
		nil,
	)
	if err != nil {
		t.Fatalf("CreateUploadPayload() unexpected error: %v", err)
	}

	var nonCodeTestFound bool
	for _, test := range payload.Tests {
		if test.Path != instrumentation.NonCodeChainPath {
			continue
		}
		nonCodeTestFound = true
		want := []string{"config/service.yaml", "pom.xml"}
		if !reflect.DeepEqual(test.IndicativeChains[0].SourcePaths, want) {
			t.Fatalf("non-code source paths = %#v, want %#v", test.IndicativeChains[0].SourcePaths, want)
		}
	}
	if !nonCodeTestFound {
		t.Fatal("expected non-code test entry in upload payload")
	}
}

func TestCreateUploadPayloadUsesDefaultSourceForEmptyNonCodeSet(t *testing.T) {
	fileChecksums := map[string]uint64{
		"src/main/java/AppTest.java":       3,
		instrumentation.NonCodeChainPath:   4,
		instrumentation.NonCodeDefaultPath: 1,
	}
	cfg := config.New("", "", "acct", "org", "proj", "", "", "", "", "", "", "", "", "", "", "", false, "", "")

	payload, err := CreateUploadPayload(
		nil,
		fileChecksums,
		instrumentation.NonCodeConfig{},
		"repo",
		&cfg,
		"sha",
		nil,
		logrus.New(),
		nil,
	)
	if err != nil {
		t.Fatalf("CreateUploadPayload() unexpected error: %v", err)
	}

	var found bool
	for _, test := range payload.Tests {
		if test.Path != instrumentation.NonCodeChainPath {
			continue
		}
		found = true
		want := []string{instrumentation.NonCodeDefaultPath}
		if !reflect.DeepEqual(test.IndicativeChains[0].SourcePaths, want) {
			t.Fatalf("non-code source paths = %#v, want %#v", test.IndicativeChains[0].SourcePaths, want)
		}
	}
	if !found {
		t.Fatal("expected non-code test entry in upload payload")
	}
	for _, chain := range payload.Chains {
		if chain.Path == instrumentation.NonCodeChainPath && chain.Checksum == "0" {
			t.Fatal("empty non-code set must use a non-zero default-source chain checksum")
		}
	}
}

func TestCreateUploadPayloadSkipsTestsWithMissingSources(t *testing.T) {
	fileChecksums := map[string]uint64{
		"src/test/java/example/CompleteTest.java": 1,
		"src/main/java/example/Service.java":      2,
		"src/test/java/example/PartialTest.java":  3,
		"src/test/java/example/OrphanTest.java":   4,
		instrumentation.NonCodeChainPath:          5,
		instrumentation.NonCodeDefaultPath:        6,
	}
	cg := &Callgraph{
		Nodes: []Node{
			{ID: 1, Type: nodeTypeTest, File: "src/test/java/example/CompleteTest.java"},
			{ID: 2, Type: "source", File: "src/main/java/example/Service.java"},
			{ID: 3, Type: nodeTypeTest, File: "src/test/java/example/PartialTest.java"},
			{ID: 4, Type: "source", File: "src/main/java/example/Generated.java"},
			{ID: 5, Type: nodeTypeTest, File: "src/test/java/example/OrphanTest.java"},
		},
		TestRelations: []Relation{
			{Source: 2, Tests: []int{1}},
			{Source: 4, Tests: []int{3}},
		},
	}
	cfg := config.New("", "", "acct", "org", "proj", "", "", "", "", "", "", "", "", "", "", "", false, "", "")

	payload, err := CreateUploadPayload(
		cg,
		fileChecksums,
		instrumentation.NonCodeConfig{},
		"repo",
		&cfg,
		"sha",
		nil,
		logrus.New(),
		nil,
	)
	if err != nil {
		t.Fatalf("CreateUploadPayload() unexpected error: %v", err)
	}

	var testPaths []string
	for _, test := range payload.Tests {
		testPaths = append(testPaths, test.Path)
	}
	var chainPaths []string
	for _, chain := range payload.Chains {
		chainPaths = append(chainPaths, chain.Path)
	}

	wantCodeTest := "src/test/java/example/CompleteTest.java"
	if !containsPath(testPaths, wantCodeTest) {
		t.Fatalf("tests = %#v, want complete test %q", testPaths, wantCodeTest)
	}
	if containsPath(testPaths, "src/test/java/example/PartialTest.java") {
		t.Fatalf("tests = %#v, partial test with missing source should be omitted", testPaths)
	}
	if containsPath(testPaths, "src/test/java/example/OrphanTest.java") {
		t.Fatalf("tests = %#v, test with no sources should be omitted", testPaths)
	}
	if !containsPath(chainPaths, wantCodeTest) {
		t.Fatalf("chains = %#v, want complete test %q", chainPaths, wantCodeTest)
	}
	if containsPath(chainPaths, "src/test/java/example/PartialTest.java") {
		t.Fatalf("chains = %#v, partial test with missing source should be omitted", chainPaths)
	}
	if containsPath(chainPaths, "src/test/java/example/OrphanTest.java") {
		t.Fatalf("chains = %#v, test with no sources should be omitted", chainPaths)
	}
}

func containsPath(paths []string, want string) bool {
	for _, path := range paths {
		if path == want {
			return true
		}
	}
	return false
}
