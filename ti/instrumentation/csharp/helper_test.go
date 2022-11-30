package csharp

import (
	"testing"

	"github.com/harness/lite-engine/ti/instrumentation/common"
	"github.com/stretchr/testify/assert"
)

func Test_ParseCsharpNode(t *testing.T) {
	testGlobs := []string{"path/to/test*/*.cs"}
	testCases := []struct {
		// Input
		FileName string
		TestGlob []string
		// Verify
		Class    string
		NodeType common.NodeType
	}{
		{"path/to/test1/t1.cs", testGlobs, "t1", common.NodeType_TEST},
		{"path/to/test2/t2.cs", testGlobs, "t2", common.NodeType_TEST},
		{"path/to/test3/t3.cs", testGlobs, "t3", common.NodeType_TEST},
		{"path/to/test4/t4.cs", testGlobs, "t4", common.NodeType_TEST},
		{"path/to/src1/s1.cs", testGlobs, "s1", common.NodeType_SOURCE},
	}
	for _, tc := range testCases {
		n, _ := ParseCsharpNode(tc.FileName, tc.TestGlob)
		assert.Equal(t, n.Class, tc.Class)
		assert.Equal(t, tc.NodeType, n.Type)
	}
}
