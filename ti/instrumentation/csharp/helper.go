package csharp

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/harness/lite-engine/ti/instrumentation/common"
	ti "github.com/harness/ti-client/types"
	"github.com/mattn/go-zglob"
	"github.com/sirupsen/logrus"
)

// GetCsharpTests returns list of RunnableTests in the workspace with cs extension.
// In case of errors, return empty list
func GetCsharpTests(workspace string, testGlobs []string) []ti.RunnableTest {
	tests := make([]ti.RunnableTest, 0)
	files, _ := common.GetFiles(fmt.Sprintf("%s/**/*.cs", workspace))
	for _, path := range files {
		if path == "" {
			continue
		}
		node, _ := ParseCsharpNode(path, testGlobs)
		if node.Type != common.NodeType_TEST {
			continue
		}
		test := ti.RunnableTest{
			Pkg:   node.Pkg,
			Class: node.Class,
		}
		tests = append(tests, test)
	}
	return tests
}

// ParseCsharpNode extracts the class name from a Dotnet file path
// e.g., src/abc/def/A.cs
// will return class = A
func ParseCsharpNode(filename string, testGlobs []string) (*common.Node, error) {
	var node common.Node
	node.Pkg = ""
	node.Class = ""
	node.Lang = common.LangType_UNKNOWN
	node.Type = common.NodeType_OTHER

	filename = strings.TrimSpace(filename)
	if !strings.HasSuffix(filename, ".cs") {
		return &node, nil
	}
	node.Lang = common.LangType_CSHARP
	node.Type = common.NodeType_SOURCE

	for _, glob := range testGlobs {
		if matched, _ := zglob.Match(glob, filename); !matched {
			continue
		}
		node.Type = common.NodeType_TEST
	}
	f := strings.TrimSuffix(filename, ".cs")
	parts := strings.Split(f, "/")
	node.Class = parts[len(parts)-1]
	return &node, nil
}

// Unzip unzips the dotnet agent zip file
// In case of errors, return the error
func Unzip(agentInstallDir string, log *logrus.Logger) (err error) {
	err = common.ExtractArchive(filepath.Join(agentInstallDir, "dotnet-agent.zip"), agentInstallDir)
	if err != nil {
		log.WithError(err).Println("could not unzip the .net agent")
		return err
	}
	return nil
}
