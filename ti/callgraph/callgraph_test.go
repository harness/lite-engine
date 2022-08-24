// Copyright 2022 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package callgraph

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCallgraph_ToStringMap(t *testing.T) {
	cg := Callgraph{
		Nodes: []Node{
			{
				Package: "package1",
				Method:  "m1",
				ID:      1,
				Params:  "param1",
				Class:   "class1",
				Type:    "source",
			},
			{
				Package:         "package2",
				Method:          "m2",
				ID:              1,
				Params:          "param2",
				Class:           "class2",
				Type:            "test",
				CallsReflection: true,
			},
		},
		TestRelations: []Relation{
			{
				Source: 0,
				Tests:  []int{1, 2, 3, 4, 5},
			},
			{
				Source: 1,
				Tests:  []int{11, 12, 13, 14, 15},
			},
		},
		VisRelations: []Relation{
			{
				Source: 2,
				Tests:  []int{2, 9, 13, 14, 15},
			},
			{
				Source: 3,
				Tests:  []int{12, 112, 113, 114, 115},
			},
		},
	}
	mp := cg.ToStringMap()

	fNodes, fRelations, vRelations := getCgObject(mp)
	finalCg := Callgraph{
		Nodes:         fNodes,
		TestRelations: fRelations,
		VisRelations:  vRelations,
	}
	assert.Equal(t, reflect.DeepEqual(finalCg, cg), true)
}

func getCgObject(mp map[string]interface{}) ([]Node, []Relation, []Relation) { //nolint:funlen,gocritic,gocyclo
	var fNodes []Node
	var fRelations, vRelations []Relation
	for k, v := range mp {
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
							node.ID = v.(int)
						case "params":
							node.Params = v.(string)
						case "class":
							node.Class = v.(string)
						case "callsReflection":
							node.CallsReflection = v.(bool)
						case "type":
							node.Type = v.(string)
						}
					}
					fNodes = append(fNodes, node)
				}
			}
		case "testRelations":
			if relations, ok := v.([]interface{}); ok {
				for _, reln := range relations {
					var relation Relation
					fields := reln.(map[string]interface{})
					for k, v := range fields {
						switch k {
						case "source":
							relation.Source = v.(int)
						case "tests":
							var testsN []int
							testsN = append(testsN, v.([]int)...)
							relation.Tests = testsN
						}
					}
					fRelations = append(fRelations, relation)
				}
			}
		case "visgraphRelations":
			if relations, ok := v.([]interface{}); ok {
				for _, reln := range relations {
					var relation Relation
					fields := reln.(map[string]interface{})
					for k, v := range fields {
						switch k {
						case "source":
							relation.Source = v.(int)
						case "destinations":
							var testsN []int
							testsN = append(testsN, v.([]int)...)
							relation.Tests = testsN
						}
					}
					vRelations = append(vRelations, relation)
				}
			}
		}
	}
	return fNodes, fRelations, vRelations
}
