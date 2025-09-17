// Copyright 2022 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package callgraph

import (
	"strings"
	"testing"

	"github.com/harness/lite-engine/internal/filesystem"
	"github.com/harness/lite-engine/ti/avro"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

func TestCallGraphParser_Parse(t *testing.T) {
	log := logrus.New()
	fs := filesystem.New()
	cgph := NewCallGraphParser(log, fs)
	dto, _ := cgph.Parse([]string{"testdata/cg.json"}, []string{})

	// Assert relations is as expected
	exp := map[int][]int{
		-776083018:  {-1648419296},
		1062598667:  {-1648419296},
		-2078257563: {1020759395},
		-1136127725: {1020759395},
		-849735784:  {1020759395},
		-1954679604: {-268233532},
		2139952358:  {-1648419296},
		330989721:   {-1648419296},
		1020759395:  {1020759395},
	}
	for _, v := range dto.TestRelations {
		assert.Equal(t, v.Tests, exp[v.Source])
	}

	// Assert the length of the Nodes parsed
	assert.Equal(t, len(dto.Nodes), 11)

	// Validate if a specific node exists in the parsed list
	sourceNode := Node{
		Package: "io.haness.exception",
		Method:  "<init>",
		ID:      2139952358,
		Params:  "java.lang.Sting,java.util.EnumSet",
		Class:   "InvalidAgumentsException",
		Type:    "source",
	}

	// Validate if a test node exists in the parsed list
	testNode := Node{
		Package:         "software.wings.sevice.intfc.signup",
		CallsReflection: true,
		Method:          "testValidateNameThowsInvalidAgumentsException",
		Params:          "void",
		Class:           "SignupSeviceTest",
		ID:              -1648419296,
		Type:            "test",
	}

	srcCnt := 0
	testCnt := 0
	for _, node := range dto.Nodes {
		if node == sourceNode {
			srcCnt++
		}
		if node == testNode {
			testCnt++
		}
	}
	assert.Equal(t, srcCnt, 1)
	assert.Equal(t, testCnt, 1)
}

func TestCallGraphParser_ParseShouldFail(t *testing.T) {
	log := logrus.New()
	fs := filesystem.New()
	cgph := NewCallGraphParser(log, fs)
	_, err := cgph.Parse([]string{"testdata/cg_invalid.json"}, []string{})

	assert.NotEqual(t, nil, err)
	assert.True(t, strings.Contains(err.Error(), "data unmarshalling to json failed for line"))
}

func TestCallGraphParser_ParseShouldFailNoFile(t *testing.T) {
	log := logrus.New()
	fs := filesystem.New()
	cgph := NewCallGraphParser(log, fs)
	_, err := cgph.Parse([]string{"testdata/cg_random.json"}, []string{})

	assert.NotEqual(t, nil, err)
	strings.Contains(err.Error(), "failed to open file")
	assert.True(t, strings.Contains(err.Error(), "failed to open file"))
}

func Test_AvroSerialize(t *testing.T) {
	log := logrus.New()
	fs := filesystem.New()
	cgParser := NewCallGraphParser(log, fs)
	cg, _ := cgParser.Parse([]string{"testdata/avro_cg.json"}, []string{})

	cgMap := cg.ToStringMap()
	cgSer, err := avro.NewCgphSerialzer(cgSchemaType, "1_1")
	assert.Equal(t, nil, err)

	encCg, _ := cgSer.Serialize(cgMap)
	des, _ := cgSer.Deserialize(encCg)
	desCg, _ := FromStringMap(des.(map[string]interface{}))
	assert.Equal(t, desCg, cg)
}
