package common

import (
	"path"
	"strings"

	ti "github.com/harness/ti-client/types"
	"github.com/mattn/go-zglob"
)

type NodeType int32

const (
	NodeType_SOURCE   NodeType = 0 //nolint:revive,stylecheck
	NodeType_TEST     NodeType = 1 //nolint:revive,stylecheck
	NodeType_CONF     NodeType = 2 //nolint:revive,stylecheck
	NodeType_RESOURCE NodeType = 3 //nolint:revive,stylecheck
	NodeType_OTHER    NodeType = 4 //nolint:revive,stylecheck
)

type LangType int32

const (
	LangType_JAVA    LangType = 0 //nolint:revive,stylecheck
	LangType_CSHARP  LangType = 1 //nolint:revive,stylecheck
	LangType_PYTHON  LangType = 2 //nolint:revive,stylecheck
	LangType_UNKNOWN LangType = 3 //nolint:revive,stylecheck
)

// Node holds data about a source code
type Node struct {
	Pkg    string
	Class  string
	Method string
	File   string
	Lang   LangType
	Type   NodeType
}

// GetFiles gets list of all file paths matching a provided regex
func GetFiles(path string) ([]string, error) {
	matches, err := zglob.Glob(path)
	if err != nil {
		return []string{}, err
	}
	return matches, err
}

// GetUniqueTestStrings extract list of test strings from Class
// It should only work if Class is the only primary identifier of the test selection
func GetUniqueTestStrings(tests []ti.RunnableTest) []string {
	// Use only unique class
	set := make(map[ti.RunnableTest]interface{})
	ut := []string{}
	for _, t := range tests {
		w := ti.RunnableTest{Class: t.Class}
		if _, ok := set[w]; ok {
			// The test has already been added
			continue
		}
		set[w] = struct{}{}
		ut = append(ut, t.Class)
	}
	return ut
}

// GetTestsFromLocal creates list of RunnableTest within file system on given globs
func GetTestsFromLocal(workspace string, testGlobs []string) ([]ti.RunnableTest, error) {
	tests := make([]ti.RunnableTest, 0)
	for _, glob := range testGlobs {
		if !strings.HasPrefix(glob, "/") {
			glob = path.Join(workspace, glob)
		}
		files, err := GetFiles(glob)
		if err != nil {
			return nil, err
		}
		for _, path := range files {
			test := ti.RunnableTest{
				Class: path,
			}
			tests = append(tests, test)
		}

	}
	return tests, nil
}
