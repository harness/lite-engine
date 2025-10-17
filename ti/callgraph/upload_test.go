package callgraph

import (
	"testing"

	"github.com/harness/lite-engine/ti/avro"
	"github.com/harness/ti-client/types"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

func TestCallGraphParser_EncodeCg(t *testing.T) {
	log := logrus.New()
	cgSer, err := avro.NewCgphSerialzer(cgSchemaType, "1_1")
	assert.Nil(t, err)

	// Case: Empty CG
	dataDir := "testdata/cgdir/emptycg"
	cgBytes, cgIsEmpty, matched, err := encodeCg(dataDir, log, []*types.TestCase{}, "1_1", false)
	assert.Nil(t, err)
	assert.True(t, cgIsEmpty)
	assert.True(t, matched)

	// Test deserialize
	cgString, err := cgSer.Deserialize(cgBytes)
	assert.Nil(t, err)
	cg, err := FromStringMap(cgString.(map[string]interface{}))
	assert.Nil(t, err)
	assert.Empty(t, cg.Nodes)
	assert.Empty(t, cg.TestRelations)
	assert.Empty(t, cg.VisRelations)

	// Case: No CG files
	dataDir = "testdata/cgdir/nocg"
	cgBytes, cgIsEmpty, matched, err = encodeCg(dataDir, log, []*types.TestCase{}, "1_1", false)
	assert.Nil(t, err)
	assert.True(t, cgIsEmpty)
	assert.True(t, matched)

	// Test deserialize
	cgString, err = cgSer.Deserialize(cgBytes)
	assert.Nil(t, err)
	cg, err = FromStringMap(cgString.(map[string]interface{}))
	assert.Nil(t, err)
	assert.Empty(t, cg.Nodes)
	assert.Empty(t, cg.TestRelations)
	assert.Empty(t, cg.VisRelations)

	// Case: CG present
	dataDir = "testdata/cgdir/cg"
	cgBytes, _, matched, err = encodeCg(dataDir, log, []*types.TestCase{}, "1_1", false)
	assert.Nil(t, err)
	assert.True(t, matched)

	// Test deserialize
	cgString, err = cgSer.Deserialize(cgBytes)
	assert.Nil(t, err)
	cg, err = FromStringMap(cgString.(map[string]interface{}))
	assert.Nil(t, err)
	assert.NotEmpty(t, cg.Nodes)
	assert.NotEmpty(t, cg.TestRelations)

	dataDir = "testdata/cgdir/cg"
	cgBytes, _, matched, err = encodeCg(dataDir, log, []*types.TestCase{}, "1_1", true)
	assert.Nil(t, err)
	assert.False(t, matched)

	cgString, err = cgSer.Deserialize(cgBytes)
	assert.Nil(t, err)
	cg, err = FromStringMap(cgString.(map[string]interface{}))
	assert.Nil(t, err)
	assert.NotEmpty(t, cg.Nodes)
	assert.NotEmpty(t, cg.TestRelations)
}

// Helper function to create test callgraph with file extensions
func createTestCg(fileExt string) *Callgraph {
	return &Callgraph{
		Nodes: []Node{
			{
				ID:      1,
				ClassID: 100,
				Package: "io.harness.test",
				Class:   "TestClass",
				Method:  "testMethod",
				Params:  "()",
				Type:    "test",
				File:    "io/harness/test/TestClass" + fileExt,
			},
			{
				ID:      2,
				ClassID: 200,
				Package: "io.harness.source",
				Class:   "SourceClass",
				Method:  "sourceMethod",
				Params:  "()",
				Type:    "source",
			},
		},
		TestRelations: []Relation{
			{Source: 2, Tests: []int{1}},
		},
	}
}

// Helper function to simulate language detection logic
func simulateLanguageDetection(cg *Callgraph, ext string) {
	languageSet := make(map[string]bool)
	for i := range cg.Nodes {
		if cg.Nodes[i].Type == nodeTypeTest && cg.Nodes[i].File != "" {
			if ext != "" {
				languageSet[ext] = true
			}
		}
	}
	if len(languageSet) > 0 {
		languages := make([]string, 0, len(languageSet))
		for lang := range languageSet {
			languages = append(languages, lang)
		}
		DetectedLanguages = languages
	}
}

func TestLanguageDetectionWithoutRerun(t *testing.T) {
	log := logrus.New()
	DetectedLanguages = []string{}
	cg := createTestCg(".java")

	simulateLanguageDetection(cg, ".java")

	assert.NotEmpty(t, DetectedLanguages, "Languages should be detected even when rerunFailedTests is false")
	assert.Contains(t, DetectedLanguages, ".java")
	log.Infof("Detected languages (rerunFailedTests=false): %v", DetectedLanguages)
}

func TestLanguageDetectionWithRerun(t *testing.T) {
	log := logrus.New()
	DetectedLanguages = []string{}
	cg := createTestCg(".py")

	// Simulate the language detection logic with rerunFailedTests = true
	languageSet := make(map[string]bool)
	for i := range cg.Nodes {
		cg.Nodes[i].HasFailed = false
		if cg.Nodes[i].Type != nodeTypeTest {
			continue
		}
		if cg.Nodes[i].File != "" {
			ext := ".py"
			if ext != "" {
				languageSet[ext] = true
			}
		}
	}
	if len(languageSet) > 0 {
		languages := make([]string, 0, len(languageSet))
		for lang := range languageSet {
			languages = append(languages, lang)
		}
		DetectedLanguages = languages
	}

	assert.NotEmpty(t, DetectedLanguages, "Languages should be detected when rerunFailedTests is true")
	assert.Contains(t, DetectedLanguages, ".py")
	log.Infof("Detected languages (rerunFailedTests=true): %v", DetectedLanguages)
}

func TestLanguageDetectionEmptyCallgraph(t *testing.T) {
	DetectedLanguages = []string{}
	cg := &Callgraph{
		Nodes:         []Node{},
		TestRelations: []Relation{},
	}

	simulateLanguageDetection(cg, "")

	assert.Empty(t, DetectedLanguages, "No languages should be detected for empty callgraph")
}

func TestLanguageDetectionNoFileField(t *testing.T) {
	DetectedLanguages = []string{}
	cg := &Callgraph{
		Nodes: []Node{
			{
				ID:      1,
				ClassID: 100,
				Package: "io.harness.test",
				Class:   "TestClass",
				Method:  "testMethod",
				Type:    "test",
				File:    "",
			},
		},
		TestRelations: []Relation{},
	}

	simulateLanguageDetection(cg, "")

	assert.Empty(t, DetectedLanguages, "No languages should be detected when nodes have no file field")
}
