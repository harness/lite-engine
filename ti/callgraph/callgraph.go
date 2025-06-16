// Copyright 2022 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

// This file has a Callgraph object which is used to upload callgraph from
// addon to ti server. It also contains helper functions, FromStringMap which
// takes avro decoded output and returns a Callgraph function. ToStringMap fn
// takes a callgraph object as input and converts it in map[string]interface{} format
// for avro encoding.

// TODO: (Vistaar) Add UT for ensuring avro formatting is correct
// Any changes made to the avro schema needs to be properly validated. This can be done
// using a avro validator online. Also, the schema needs to be converted to a go file
// using a tool like go-bindata.
// go-bindata -o callgraph.go callgraph.avsc
// Without this, none of the changes will take effect.

package callgraph

import (
	"errors"
	"fmt"
)

// Callgraph object is used for data transfer b/w ti service and lite-engine
type Callgraph struct {
	Nodes         []Node
	TestRelations []Relation
	VisRelations  []Relation
}

// ToStringMap converts Callgraph to map[string]interface{} for encoding
func (cg *Callgraph) ToStringMap() map[string]interface{} {
	var nodes, tRelations, vRelations []interface{}
	for _, v := range cg.Nodes {
		data := map[string]interface{}{
			"package":         v.Package,
			"method":          v.Method,
			"id":              v.ID,
			"classId":         v.ClassID,
			"params":          v.Params,
			"class":           v.Class,
			"type":            v.Type,
			"callsReflection": v.CallsReflection,
			"alwaysRun":       v.AlwaysRun,
			"file":            v.File,
			"hasFailed":       v.HasFailed,
		}
		nodes = append(nodes, data)
	}
	for _, v := range cg.TestRelations {
		data := map[string]interface{}{
			"source": v.Source,
			"tests":  v.Tests,
		}
		tRelations = append(tRelations, data)
	}
	for _, v := range cg.VisRelations {
		data := map[string]interface{}{
			"source":       v.Source,
			"destinations": v.Tests,
		}
		vRelations = append(vRelations, data)
	}
	data := map[string]interface{}{
		"nodes":             nodes,
		"testRelations":     tRelations,
		"visgraphRelations": vRelations,
	}
	return data
}

// FromStringMap creates Callgraph object from map[string]interface{}
func FromStringMap(data map[string]interface{}) (*Callgraph, error) { //nolint:gocyclo
	var fNodes []Node
	var fRel, vRel []Relation
	for k, v := range data {
		switch k {
		case "nodes":
			if nodes, ok := v.([]interface{}); ok {
				for _, t := range nodes {
					fields := t.(map[string]interface{})
					var node Node
					for f, v := range fields {
						switch f {
						case "method":
							node.Method = v.(string)
						case "package":
							node.Package = v.(string)
						case "id":
							node.ID = int(v.(int32))
						case "classId":
							node.ClassID = int(v.(int32))
						case "params":
							node.Params = v.(string)
						case "class":
							node.Class = v.(string)
						case "callsReflection":
							node.CallsReflection = v.(bool)
						case "alwaysRun":
							node.AlwaysRun = v.(bool)
						case "type":
							node.Type = v.(string)
						case "file":
							node.File = v.(string)
						default:
							return nil, fmt.Errorf("unknown field received: %s", f)
						}
					}
					fNodes = append(fNodes, node)
				}
			} else {
				return nil, errors.New("failed to parse nodes in callgraph")
			}
		case "testRelations":
			if relns, ok := v.([]interface{}); ok {
				rel, err := convertStringMap("source", "tests", relns)
				if err != nil {
					return nil, err
				}
				fRel = *rel
			} else {
				return nil, errors.New("failed to parse test relns in callgraph")
			}
		case "visgraphRelations":
			if relns, ok := v.([]interface{}); ok {
				rel, err := convertStringMap("source", "destinations", relns)
				if err != nil {
					return nil, err
				}
				vRel = *rel
			} else {
				return nil, errors.New("failed to parse vis relns in callgraph")
			}
		}
	}
	return &Callgraph{
		TestRelations: fRel,
		Nodes:         fNodes,
		VisRelations:  vRel,
	}, nil
}

func convertStringMap(key, val string, relns []interface{}) (*[]Relation, error) {
	var vRel []Relation
	for _, reln := range relns {
		var relation Relation
		fields := reln.(map[string]interface{})
		for k, v := range fields {
			switch k {
			case key:
				relation.Source = int(v.(int32))
			case val:
				var testsN []int
				for _, v := range v.([]interface{}) {
					testsN = append(testsN, int(v.(int32)))
				}
				relation.Tests = testsN
			default:
				return nil, fmt.Errorf("unknown field received: %s", k)
			}
		}
		vRel = append(vRel, relation)
	}
	return &vRel, nil
}

// Node type represents detail of node in callgraph
type Node struct {
	Package         string
	Method          string
	ID              int
	ClassID         int
	Params          string
	Class           string
	Type            string
	CallsReflection bool
	AlwaysRun       bool
	File            string
	HasFailed       bool
}

// Input is the go representation of each line in callgraph file
type Input struct {
	Test     Node
	Source   Node
	Resource Node
}

// Relation b/w source and test
type Relation struct {
	Source int
	Tests  []int
}
