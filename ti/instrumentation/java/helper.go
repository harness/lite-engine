// Copyright 2022 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package java

import (
	"bufio"
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/mattn/go-zglob"
	"github.com/sirupsen/logrus"

	"github.com/harness/lite-engine/internal/filesystem"
)

const (
	AgentArg     = "-javaagent:%s=%s"
	JavaAgentJar = "java-agent.jar"
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

const (
	JAVA_SRC_PATH      = "src/main/java/"      //nolint:revive,stylecheck
	JAVA_TEST_PATH     = "src/test/java/"      //nolint:revive,stylecheck
	JAVA_RESOURCE_PATH = "src/test/resources/" //nolint:revive,stylecheck
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

// get list of all file paths matching a provided regex
func getFiles(path string) ([]string, error) {
	matches, err := zglob.Glob(path)
	if err != nil {
		return []string{}, err
	}
	return matches, err
}

// ParseJavaNode extracts the pkg and class names from a Java file path
// e.g., 320-ci-execution/src/main/java/io/harness/stateutils/buildstate/ConnectorUtils.java
// will return pkg = io.harness.stateutils.buildstate, class = ConnectorUtils
func ParseJavaNode(filename string) (*Node, error) {
	var node Node
	node.Pkg = ""
	node.Class = ""
	node.Lang = LangType_UNKNOWN
	node.Type = NodeType_OTHER

	filename = strings.TrimSpace(filename)

	var r *regexp.Regexp
	if strings.Contains(filename, JAVA_SRC_PATH) && strings.HasSuffix(filename, ".java") {
		r = regexp.MustCompile(`^.*src/main/java/`)
		node.Type = NodeType_SOURCE
		rr := r.ReplaceAllString(filename, "${1}") // extract the 2nd part after matching the src/main/java prefix
		rr = strings.TrimSuffix(rr, ".java")

		parts := strings.Split(rr, "/")
		p := parts[:len(parts)-1]
		node.Class = parts[len(parts)-1]
		node.Lang = LangType_JAVA
		node.Pkg = strings.Join(p, ".")
	} else if strings.Contains(filename, JAVA_TEST_PATH) && strings.HasSuffix(filename, ".java") {
		r = regexp.MustCompile(`^.*src/test/java/`)
		node.Type = NodeType_TEST
		rr := r.ReplaceAllString(filename, "${1}") // extract the 2nd part after matching the src/test/java prefix
		rr = strings.TrimSuffix(rr, ".java")

		parts := strings.Split(rr, "/")
		p := parts[:len(parts)-1]
		node.Class = parts[len(parts)-1]
		node.Lang = LangType_JAVA
		node.Pkg = strings.Join(p, ".")
	} else if strings.Contains(filename, JAVA_RESOURCE_PATH) {
		node.Type = NodeType_RESOURCE
		parts := strings.Split(filename, "/")
		node.File = parts[len(parts)-1]
		node.Lang = LangType_JAVA
	} else if strings.HasSuffix(filename, ".scala") {
		// Cannot make any assumption from scala/kotlin file path, return generic source node for now
		node.Lang = LangType_JAVA
		node.Type = NodeType_SOURCE
		f := strings.TrimSuffix(filename, ".scala")
		parts := strings.Split(f, "/")
		node.Class = parts[len(parts)-1]
	} else if strings.HasSuffix(filename, ".kt") {
		node.Lang = LangType_JAVA
		node.Type = NodeType_SOURCE
		f := strings.TrimSuffix(filename, ".kt")
		parts := strings.Split(f, "/")
		node.Class = parts[len(parts)-1]
	} else {
		return &node, nil
	}

	return &node, nil
}

// detect java packages by reading all the files and parsing their package names
func DetectPkgs(workspace string, log *logrus.Logger, fs filesystem.FileSystem) ([]string, error) { //nolint:gocyclo
	plist := []string{}
	excludeList := []string{"com.google"} // exclude any instances of these packages from the package list

	files, err := getFiles(fmt.Sprintf("%s/**/*.java", workspace))
	if err != nil {
		return plist, err
	}
	kotlinFiles, err := getFiles(fmt.Sprintf("%s/**/*.kt", workspace))
	if err != nil {
		return plist, err
	}
	// Create a list with all *.java and *.kt file paths
	files = append(files, kotlinFiles...)
	fmt.Println("files: ", files)
	m := make(map[string]struct{})
	for _, f := range files {
		absPath, err := filepath.Abs(f)
		if err != nil {
			log.WithError(err).WithField("file", f).Errorln("could not get absolute path")
			continue
		}
		// TODO: (Vistaar)
		// This doesn't handle some special cases right now such as when there is a package
		// present in a multiline comment with multiple opening and closing comments.
		// We will require to read all the lines together to handle this.
		err = fs.ReadFile(absPath, func(fr io.Reader) error {
			scanner := bufio.NewScanner(fr)
			commentOpen := false
			for scanner.Scan() {
				l := strings.TrimSpace(scanner.Text())
				if strings.Contains(l, "/*") {
					commentOpen = true
				}
				if strings.Contains(l, "*/") {
					commentOpen = false
					continue
				}
				if commentOpen || strings.HasPrefix(l, "//") {
					continue
				}
				prev := ""
				pkg := ""
				for _, token := range strings.Fields(l) {
					if prev == "package" {
						pkg = token
						break
					}
					prev = token
				}
				if pkg != "" {
					pkg = strings.TrimSuffix(pkg, ";")
					tokens := strings.Split(pkg, ".")
					prefix := false
					for _, exclude := range excludeList {
						if strings.HasPrefix(pkg, exclude) {
							logrus.Infoln(fmt.Sprintf("Found package: %s having same package prefix as: %s. Excluding this package from the list...", pkg, exclude))
							prefix = true
							break
						}
					}
					if !prefix {
						pkg = tokens[0]
						if len(tokens) > 1 {
							pkg = pkg + "." + tokens[1]
						}
					}
					if pkg == "" {
						continue
					}
					if _, ok := m[pkg]; !ok {
						plist = append(plist, pkg)
						m[pkg] = struct{}{}
					}
					return nil
				}
			}
			if err = scanner.Err(); err != nil {
				logrus.WithError(err).Errorln("could not scan all the files")
				return err
			}
			return nil
		})
		if err != nil {
			logrus.WithError(err).Errorln("had issues while trying to auto detect java packages")
		}
	}
	return plist, nil
}
