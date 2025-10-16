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

func TestLanguageDetection(t *testing.T) {
	log := logrus.New()

	// Reset DetectedLanguages before each test
	DetectedLanguages = []string{}

	// Test 1: Language detection with rerunFailedTests = false
	t.Run("Language detection without rerun failed tests", func(t *testing.T) {
		DetectedLanguages = []string{}
		dataDir := "testdata/cgdir/cg"
		_, _, _, err := encodeCg(dataDir, log, []*types.TestCase{}, "1_1", false)
		assert.Nil(t, err)
		// Verify that languages were detected even when rerunFailedTests is false
		assert.NotEmpty(t, DetectedLanguages, "Languages should be detected even when rerunFailedTests is false")
		log.Infof("Detected languages (rerunFailedTests=false): %v", DetectedLanguages)
	})

	// Test 2: Language detection with rerunFailedTests = true
	t.Run("Language detection with rerun failed tests", func(t *testing.T) {
		DetectedLanguages = []string{}
		dataDir := "testdata/cgdir/cg"
		_, _, _, err := encodeCg(dataDir, log, []*types.TestCase{}, "1_1", true)
		assert.Nil(t, err)
		// Verify that languages were detected
		assert.NotEmpty(t, DetectedLanguages, "Languages should be detected when rerunFailedTests is true")
		log.Infof("Detected languages (rerunFailedTests=true): %v", DetectedLanguages)
	})

	// Test 3: No languages detected for empty callgraph
	t.Run("No languages for empty callgraph", func(t *testing.T) {
		DetectedLanguages = []string{}
		dataDir := "testdata/cgdir/emptycg"
		_, _, _, err := encodeCg(dataDir, log, []*types.TestCase{}, "1_1", false)
		assert.Nil(t, err)
		// Verify that no languages were detected for empty callgraph
		assert.Empty(t, DetectedLanguages, "No languages should be detected for empty callgraph")
	})

	// Test 4: No languages detected when no callgraph files exist
	t.Run("No languages when no callgraph files", func(t *testing.T) {
		DetectedLanguages = []string{}
		dataDir := "testdata/cgdir/nocg"
		_, _, _, err := encodeCg(dataDir, log, []*types.TestCase{}, "1_1", false)
		assert.Nil(t, err)
		// Verify that no languages were detected
		assert.Empty(t, DetectedLanguages, "No languages should be detected when no callgraph files exist")
	})
}
