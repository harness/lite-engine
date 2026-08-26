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
