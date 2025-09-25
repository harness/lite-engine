// Copyright 2022 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package callgraph

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestDetectLanguageFromFile(t *testing.T) {
	tests := []struct {
		name     string
		filePath string
		expected string
	}{
		{"Python file", "test.py", "python"},
		{"Java file", "Test.java", "java"},
		{"Kotlin file", "Test.kt", "kotlin"},
		{"Kotlin script", "Test.kts", "kotlin"},
		{"Scala file", "Test.scala", "scala"},
		{"C# file", "Test.cs", "dotnet"},
		{"VB file", "Test.vb", "dotnet"},
		{"F# file", "Test.fs", "dotnet"},
		{"Ruby file", "test.rb", "ruby"},
		{"Unknown file", "test.txt", "unknown"},
		{"No extension", "test", "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := detectLanguageFromFile(tt.filePath)
			if result != tt.expected {
				t.Errorf("detectLanguageFromFile(%s) = %s, want %s", tt.filePath, result, tt.expected)
			}
		})
	}
}



func TestDetectLanguageFromCallgraphData(t *testing.T) {
	// Full callgraph data as provided
	callgraphData := `{"test":{"id":1547676894,"classId":1547676894,"class":"tests/test_calculator1.py","file":"tests/test_calculator1.py"},"source":{"id":483305205,"classId":483305205,"class":"calculator1.py","file":"calculator1.py"}}
{"test":{"id":1547676894,"classId":1547676894,"class":"tests/test_calculator1.py","file":"tests/test_calculator1.py"},"source":{"id":1547676894,"classId":1547676894,"class":"tests/test_calculator1.py","file":"tests/test_calculator1.py"}}
{"test":{"id":1191630458,"classId":1191630458,"class":"tests/test_calculator10.py","file":"tests/test_calculator10.py"},"source":{"id":1886757882,"classId":1886757882,"class":"calculator10.py","file":"calculator10.py"}}
{"test":{"id":1191630458,"classId":1191630458,"class":"tests/test_calculator10.py","file":"tests/test_calculator10.py"},"source":{"id":1191630458,"classId":1191630458,"class":"tests/test_calculator10.py","file":"tests/test_calculator10.py"}}
{"test":{"id":1014818377,"classId":1014818377,"class":"tests/test_calculator100.py","file":"tests/test_calculator100.py"},"source":{"id":1014818377,"classId":1014818377,"class":"tests/test_calculator100.py","file":"tests/test_calculator100.py"}}
{"test":{"id":1014818377,"classId":1014818377,"class":"tests/test_calculator100.py","file":"tests/test_calculator100.py"},"source":{"id":372773554,"classId":372773554,"class":"calculator100.py","file":"calculator100.py"}}`

	// Parse the callgraph data
	lines := strings.Split(strings.TrimSpace(callgraphData), "\n")
	
	type CallgraphEntry struct {
		Test   Node `json:"test"`
		Source Node `json:"source"`
	}
	
	var nodes []Node
	for _, line := range lines {
		var entry CallgraphEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("Failed to parse callgraph line: %v", err)
		}
		nodes = append(nodes, entry.Test, entry.Source)
	}
	
	// Create callgraph with all nodes
	cg := &Callgraph{Nodes: nodes}
	
	// Test language detection
	result := DetectLanguageFromFirstNode(cg)
	if result != "python" {
		t.Errorf("DetectLanguageFromFirstNode() with full callgraph = %s, want python", result)
	}
	
	// Test with individual files from the callgraph
	testFiles := []string{
		"tests/test_calculator1.py",
		"calculator1.py", 
		"tests/test_calculator10.py",
		"calculator10.py",
		"tests/test_calculator100.py",
		"calculator100.py",
	}
	
	for _, file := range testFiles {
		result := detectLanguageFromFile(file)
		if result != "python" {
			t.Errorf("detectLanguageFromFile(%s) = %s, want python", file, result)
		}
	}
}

func TestDetectLanguageFromFirstNode(t *testing.T) {
	tests := []struct {
		name     string
		callgraph *Callgraph
		expected string
	}{
		{
			name:      "nil callgraph",
			callgraph: nil,
			expected:  "unknown",
		},
		{
			name:      "empty callgraph",
			callgraph: &Callgraph{Nodes: []Node{}},
			expected:  "unknown",
		},
		{
			name: "python test file",
			callgraph: &Callgraph{
				Nodes: []Node{
					{
						ID:      1547676894,
						ClassID: 1547676894,
						Class:   "tests/test_calculator1.py",
						File:    "tests/test_calculator1.py",
					},
				},
			},
			expected: "python",
		},
		{
			name: "python source file",
			callgraph: &Callgraph{
				Nodes: []Node{
					{
						ID:      483305205,
						ClassID: 483305205,
						Class:   "calculator1.py",
						File:    "calculator1.py",
					},
				},
			},
			expected: "python",
		},
		{
			name: "java file",
			callgraph: &Callgraph{
				Nodes: []Node{
					{
						ID:      123456,
						ClassID: 123456,
						Class:   "Calculator.java",
						File:    "src/Calculator.java",
					},
				},
			},
			expected: "java",
		},
		{
			name: "empty file",
			callgraph: &Callgraph{
				Nodes: []Node{
					{File: ""},
				},
			},
			expected: "unknown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := DetectLanguageFromFirstNode(tt.callgraph)
			if result != tt.expected {
				t.Errorf("DetectLanguageFromFirstNode() = %s, want %s", result, tt.expected)
			}
		})
	}
}