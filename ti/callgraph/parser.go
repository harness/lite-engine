// Copyright 2022 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

// This file implements a parser for callgraph file. It reads all the files
// in the callgraph directory, dedupes the data, and then returns Callgraph object
package callgraph

import (
	"bufio"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/harness/lite-engine/internal/filesystem"
	"github.com/sirupsen/logrus"
)

// Parser reads callgraph file, processes it to extract
// nodes and relations
type Parser interface {
	// Parse and read the file from
	Parse(cgFile, visFile []string) (*Callgraph, error)
}

// CallGraphParser struct definition
type CallGraphParser struct { //nolint:revive
	log *logrus.Logger        // logger
	fs  filesystem.FileSystem // filesystem handler
}

// NewCallGraphParser creates a new CallGraphParser client and returns it
func NewCallGraphParser(log *logrus.Logger, fs filesystem.FileSystem) *CallGraphParser {
	return &CallGraphParser{
		log: log,
		fs:  fs,
	}
}

// Iterate through all the cg files in the directory, parse each of them and return Callgraph object
func (cg *CallGraphParser) Parse(cgFiles, visFiles []string) (*Callgraph, error) {
	cgraph, err := cg.parseCg(cgFiles)
	if err != nil {
		return nil, err
	}
	visRelation, err := cg.parseVis(visFiles)
	if err != nil {
		return nil, err
	}
	return &Callgraph{
		Nodes:         cgraph.Nodes,
		TestRelations: cgraph.TestRelations,
		VisRelations:  *visRelation,
	}, nil
}

// parseCg parses callgraph data from list of strings
func (cg *CallGraphParser) parseCg(files []string) (*Callgraph, error) {
	cgList, err := cg.readFiles(files)
	if err != nil {
		return nil, err
	}
	cgraph, err := parseCg(cgList)
	if err != nil {
		return nil, err
	}
	return cgraph, nil
}

// read list of files, merge all of them and returns array of strings where each string is one line of file
func (cg *CallGraphParser) readFiles(files []string) ([]string, error) {
	var finalData []string
	for _, file := range files {
		f, err := cg.fs.Open(file)
		if err != nil {
			return []string{}, fmt.Errorf("failed to open file %s: %w", file, err)
		}
		r := bufio.NewReader(f)
		cgStr, err := rFile(r)
		if err != nil {
			return []string{}, fmt.Errorf("failed to parse file %s: %w", file, err)
		}
		finalData = append(finalData, cgStr...)
	}
	return finalData, nil
}

// reads visualization callgraph files and converts it into relation object
func (cg *CallGraphParser) parseVis(visFiles []string) (*[]Relation, error) {
	vgList, err := cg.readFiles(visFiles)
	if err != nil {
		return nil, err
	}
	return formatVG(vgList)
}

// convert line of visgrph file to relation object
// dedupe data - string format - `{1, 2}`
func formatVG(vg []string) (*[]Relation, error) {
	var keys, values []int
	var relnList []Relation
	for _, row := range vg {
		s := strings.Split(row, ",")
		key, value, err := getNodes(s)
		if err != nil {
			return nil, err
		}
		keys = append(keys, key)
		values = append(values, value)
	}

	// deduping records
	m := make(map[int][]int)
	for i, key := range keys {
		m[key] = append(m[key], values[i])
	}

	for k, v := range m {
		relnList = append(relnList, Relation{
			Source: k,
			Tests:  removeDup(v),
		})
	}
	return &relnList, nil
}

// removed duplicate elements from slice
func removeDup(s []int) []int {
	tmp := make(map[int]bool)
	var c []int
	for i := range s {
		tmp[s[i]] = true
	}
	for k := range tmp {
		c = append(c, k)
	}
	return c
}

// parses one line of visGraph file
// format - [-841968839,1459543895]
func getNodes(s []string) (int, int, error) { //nolint:gocritic
	if len(s) != 2 { //nolint:mnd
		return 0, 0, fmt.Errorf("parsing failed: string format is not correct %v", s)
	}
	key, err1 := strconv.Atoi(s[0])
	val, err2 := strconv.Atoi(s[1])
	if err1 != nil || err2 != nil {
		return 0, 0, fmt.Errorf("parsing failed: Id format is not correct %s %s", s[0], s[1])
	}
	return key, val, nil
}

// parseCg reads the input callgraph file and converts it into callgraph object
func parseCg(cgStr []string) (*Callgraph, error) {
	var (
		err error
		inp []Input
	)
	for _, line := range cgStr {
		tinp := &Input{}
		err = json.Unmarshal([]byte(line), tinp)
		if err != nil {
			return nil, fmt.Errorf("data unmarshalling to json failed for line [%s]: %w", line, err)
		}
		inp = append(inp, *tinp)
	}
	return process(inp), nil
}

func process(inps []Input) *Callgraph {
	var relns []Relation
	var nodes []Node

	relMap, nodeMap := convCgph(inps)
	// Updating the Relations map
	for k, v := range relMap {
		tRel := Relation{
			Source: k,
			Tests:  v,
		}
		relns = append(relns, tRel)
	}

	// Updating the nodes map
	for _, v := range nodeMap { //nolint:gocritic // rangeValCopy: copy is intentional for aggregation
		nodes = append(nodes, v)
	}
	return &Callgraph{
		Nodes:         nodes,
		TestRelations: relns,
	}
}

func convCgph(inps []Input) (map[int][]int, map[int]Node) { //nolint:gocritic
	relMap := make(map[int][]int)
	nodeMap := make(map[int]Node)

	for _, inp := range inps { //nolint:gocritic
		// processing nodeMap
		test := inp.Test
		test.Type = nodeTypeTest
		testID := test.ID
		nodeMap[testID] = test
		// processing relmap
		var source Node
		if inp.Source == (Node{}) {
			source = inp.Resource
			source.Type = "resource"
		} else {
			source = inp.Source
			source.Type = "source" //nolint:goconst
		}
		sourceID := source.ID
		_, ok := nodeMap[sourceID]
		// Do not overwrite to source if already exist as test
		if !ok {
			nodeMap[sourceID] = source
		}
		relMap[sourceID] = append(relMap[sourceID], testID)
	}
	return relMap, nodeMap
}

// rLine reads line in callgraph file which corresponds to one entry of callgraph
// had to use bufio reader as the scanner.Scan() fn has limitation
// over the number of bytes it can read and was not working on callgraph file.
func rLine(r *bufio.Reader) (string, error) {
	var (
		isPrefix = true
		err      error
		line, ln []byte
	)
	for isPrefix && err == nil {
		line, isPrefix, err = r.ReadLine()
		ln = append(ln, line...)
	}
	return string(ln), err
}

// rFile reads callgraph file
func rFile(r *bufio.Reader) ([]string, error) { //nolint:unparam
	var ret []string
	s, e := rLine(r)
	for e == nil {
		ret = append(ret, s)
		s, e = rLine(r)
	}
	return ret, nil
}
