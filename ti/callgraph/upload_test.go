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
	cgSer, err := avro.NewCgphSerialzer(cgSchemaType)
	assert.Nil(t, err)

	// Case: Empty CG
	dataDir := "testdata/cgdir/emptycg"
	cgBytes, cgIsEmpty, err := encodeCg(dataDir, log, []*types.TestCase{})
	assert.Nil(t, err)
	assert.True(t, cgIsEmpty)

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
	cgBytes, cgIsEmpty, err = encodeCg(dataDir, log, []*types.TestCase{})
	assert.Nil(t, err)
	assert.True(t, cgIsEmpty)

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
	cgBytes, _, err = encodeCg(dataDir, log, []*types.TestCase{})
	assert.Nil(t, err)

	// Test deserialize
	cgString, err = cgSer.Deserialize(cgBytes)
	assert.Nil(t, err)
	cg, err = FromStringMap(cgString.(map[string]interface{}))
	assert.Nil(t, err)
	assert.NotEmpty(t, cg.Nodes)
	assert.NotEmpty(t, cg.TestRelations)
}
