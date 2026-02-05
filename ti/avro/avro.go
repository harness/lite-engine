// Copyright 2022 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package avro

import (
	"fmt"

	goavro "github.com/linkedin/goavro/v2"

	cg "github.com/harness/lite-engine/ti/avro/schema/callgraph"
	cg_1_1 "github.com/harness/lite-engine/ti/avro/schema/callgraph_1_1"
)

// Serialzer is the interface for encoding and decoding structs
type Serialzer interface {
	// Serialize given struct and return the binary value
	Serialize(datum interface{}) ([]byte, error)
	// Deserialize given struct and return the decoded interface{}
	Deserialize(buf []byte) (interface{}, error)
}

// CgphSerialzer struct implementing NewCgphSer interface
type CgphSerialzer struct {
	codec *goavro.Codec
}

const (
	cgType             = "callgraph"
	cgSrcFile          = "callgraph.avsc"
	cgSrcFileVersioned = "callgraph_%s.avsc"
	// vgType    = "visgraph"
	// vgSrcFile = "visgraph.avsc"
)

// NewCgphSerialzer returns new CgphSerialzer object with the codec
// based on the schema received in the input
func NewCgphSerialzer(typ, version string) (*CgphSerialzer, error) {
	var schema []byte
	var err error
	switch typ {
	case cgType:
		switch version {
		case "":
			schema, err = cg.Asset(cgSrcFile)
		case "1_1":
			schema, err = cg_1_1.Asset(fmt.Sprintf(cgSrcFileVersioned, version))
		}
	default:
		return nil, fmt.Errorf("type %s is not supported", typ)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read schema file: %w", err)
	}

	codec, err := goavro.NewCodec(string(schema))
	if err != nil {
		panic(err)
	}

	return &CgphSerialzer{
		codec: codec,
	}, nil
}

// Serialize a given struct interface and return byte array and error
func (c *CgphSerialzer) Serialize(datum interface{}) ([]byte, error) {
	bin, err := c.codec.BinaryFromNative(nil, datum)
	if err != nil {
		return nil, fmt.Errorf("failed to encode the data: %w", err)
	}
	return bin, nil
}

// Deserialize a interface and return a Byte array which can be converted into corresponding struct
func (c *CgphSerialzer) Deserialize(buf []byte) (interface{}, error) {
	native, _, err := c.codec.NativeFromBinary(buf)
	if err != nil {
		return nil, err
	}
	return native, nil
}
