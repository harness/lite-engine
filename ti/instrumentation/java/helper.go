// Copyright 2022 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package java

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/harness/lite-engine/internal/filesystem"
	"github.com/harness/lite-engine/ti"
	"github.com/harness/lite-engine/ti/instrumentation/common"
	"github.com/sirupsen/logrus"
)

const (
	AgentArg     = "-javaagent:%s=%s"
	JavaAgentJar = "java-agent.jar"
	JavaStaticAgentJar = "source-code-analysis-java.jar"
)

const (
	JAVA_SRC_PATH      = "src/main/java/"      //nolint:revive,stylecheck
	JAVA_TEST_PATH     = "src/test/java/"      //nolint:revive,stylecheck
	JAVA_RESOURCE_PATH = "src/test/resources/" //nolint:revive,stylecheck
	SCALA_TEST_PATH    = "src/test/scala/"     //nolint:revive,stylecheck
	KOTLIN_TEST_PATH   = "src/test/kotlin/"    //nolint:revive,stylecheck
)

var (
	javaSourceRegex = fmt.Sprintf("^.*%s", JAVA_SRC_PATH)
	javaTestRegex   = fmt.Sprintf("^.*%s", JAVA_TEST_PATH)
	scalaTestRegex  = fmt.Sprintf("^.*%s", SCALA_TEST_PATH)
	kotlinTestRegex = fmt.Sprintf("^.*%s", KOTLIN_TEST_PATH)
)

// GetJavaTests returns list of RunnableTests in the workspace with java extension.
// In case of errors, return empty list
func GetJavaTests(workspace string, testGlobs []string) []ti.RunnableTest {
	tests := make([]ti.RunnableTest, 0)
	files, _ := common.GetFiles(fmt.Sprintf("%s/**/*.java", workspace))
	for _, path := range files {
		if path == "" {
			continue
		}
		node, _ := ParseJavaNode(path, testGlobs)
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

// GetScalaTests returns list of RunnableTests in the workspace with scala extension.
// In case of errors, return empty list
func GetScalaTests(workspace string, testGlobs []string) []ti.RunnableTest {
	tests := make([]ti.RunnableTest, 0)
	files, _ := common.GetFiles(fmt.Sprintf("%s/**/*.scala", workspace))
	for _, path := range files {
		if path == "" {
			continue
		}
		node, _ := ParseJavaNode(path, testGlobs)
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

// GetKotlinTests returns list of RunnableTests in the workspace with kotlin extension.
// In case of errors, return empty list
func GetKotlinTests(workspace string, testGlobs []string) []ti.RunnableTest {
	tests := make([]ti.RunnableTest, 0)
	files, _ := common.GetFiles(fmt.Sprintf("%s/**/*.kt", workspace))
	for _, path := range files {
		if path == "" {
			continue
		}
		node, _ := ParseJavaNode(path, testGlobs)
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

// ParseJavaNode extracts the pkg and class names from a Java file path
// e.g., 320-ci-execution/src/main/java/io/harness/stateutils/buildstate/ConnectorUtils.java
// will return pkg = io.harness.stateutils.buildstate, class = ConnectorUtils
func ParseJavaNode(filename string, testGlobs []string) (*common.Node, error) {
	var node common.Node
	node.Pkg = ""
	node.Class = ""
	node.Lang = common.LangType_UNKNOWN
	node.Type = common.NodeType_OTHER

	filename = strings.TrimSpace(filename)

	var r *regexp.Regexp
	if strings.Contains(filename, JAVA_SRC_PATH) && strings.HasSuffix(filename, ".java") {
		r = regexp.MustCompile(javaSourceRegex)
		node.Type = common.NodeType_SOURCE
		rr := r.ReplaceAllString(filename, "${1}") // extract the 2nd part after matching the src/main/java prefix
		rr = strings.TrimSuffix(rr, ".java")

		parts := strings.Split(rr, "/")
		p := parts[:len(parts)-1]
		node.Class = parts[len(parts)-1]
		node.Lang = common.LangType_JAVA
		node.Pkg = strings.Join(p, ".")
	} else if strings.Contains(filename, JAVA_TEST_PATH) && strings.HasSuffix(filename, ".java") {
		r = regexp.MustCompile(javaTestRegex)
		node.Type = common.NodeType_TEST
		rr := r.ReplaceAllString(filename, "${1}") // extract the 2nd part after matching the src/test/java prefix
		rr = strings.TrimSuffix(rr, ".java")

		parts := strings.Split(rr, "/")
		p := parts[:len(parts)-1]
		node.Class = parts[len(parts)-1]
		node.Lang = common.LangType_JAVA
		node.Pkg = strings.Join(p, ".")
	} else if strings.Contains(filename, JAVA_RESOURCE_PATH) {
		node.Type = common.NodeType_RESOURCE
		parts := strings.Split(filename, "/")
		node.File = parts[len(parts)-1]
		node.Lang = common.LangType_JAVA
	} else if strings.HasSuffix(filename, ".scala") {
		// If the scala filepath does not match any of the test paths below, return generic source node
		node.Type = common.NodeType_SOURCE
		node.Lang = common.LangType_JAVA
		f := strings.TrimSuffix(filename, ".scala")
		parts := strings.Split(f, "/")
		node.Class = parts[len(parts)-1]
		// Check for Test Node
		if strings.Contains(filename, SCALA_TEST_PATH) {
			r = regexp.MustCompile(scalaTestRegex)
			node.Type = common.NodeType_TEST
			rr := r.ReplaceAllString(f, "${1}")

			parts = strings.Split(rr, "/")
			p := parts[:len(parts)-1]
			node.Pkg = strings.Join(p, ".")
		} else if strings.Contains(filename, JAVA_TEST_PATH) {
			r = regexp.MustCompile(javaTestRegex)
			node.Type = common.NodeType_TEST
			rr := r.ReplaceAllString(f, "${1}")

			parts = strings.Split(rr, "/")
			p := parts[:len(parts)-1]
			node.Pkg = strings.Join(p, ".")
		}
	} else if strings.HasSuffix(filename, ".kt") {
		// If the kotlin filepath does not match any of the test paths below, return generic source node
		node.Type = common.NodeType_SOURCE
		node.Lang = common.LangType_JAVA
		f := strings.TrimSuffix(filename, ".kt")
		parts := strings.Split(f, "/")
		node.Class = parts[len(parts)-1]
		// Check for Test Node
		if strings.Contains(filename, KOTLIN_TEST_PATH) {
			r = regexp.MustCompile(kotlinTestRegex)
			node.Type = common.NodeType_TEST
			rr := r.ReplaceAllString(f, "${1}")

			parts = strings.Split(rr, "/")
			p := parts[:len(parts)-1]
			node.Pkg = strings.Join(p, ".")
		} else if strings.Contains(filename, JAVA_TEST_PATH) {
			r = regexp.MustCompile(javaTestRegex)
			node.Type = common.NodeType_TEST
			rr := r.ReplaceAllString(f, "${1}")

			parts = strings.Split(rr, "/")
			p := parts[:len(parts)-1]
			node.Pkg = strings.Join(p, ".")
		}
	} else {
		return &node, nil
	}

	return &node, nil
}

// detect java packages by reading all the files and parsing their package names
func DetectPkgs(workspace string, log *logrus.Logger, fs filesystem.FileSystem) ([]string, error) { //nolint:gocyclo
	plist := []string{}
	excludeList := []string{"com.google"} // exclude any instances of these packages from the package list

	files, err := common.GetFiles(fmt.Sprintf("%s/**/*.java", workspace))
	if err != nil {
		return plist, err
	}
	kotlinFiles, err := common.GetFiles(fmt.Sprintf("%s/**/*.kt", workspace))
	if err != nil {
		return plist, err
	}
	scalaFiles, err := common.GetFiles(fmt.Sprintf("%s/**/*.scala", workspace))
	if err != nil {
		return plist, err
	}
	// Create a list with all *.java, *.kt and *.scala file paths
	files = append(files, kotlinFiles...)
	files = append(files, scalaFiles...)
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

func parseBazelTestRule(r string) (ti.RunnableTest, error) {
	// r = //module:package.class
	if r == "" {
		return ti.RunnableTest{}, fmt.Errorf("empty rule")
	}
	n := 2
	if !strings.Contains(r, ":") || len(strings.Split(r, ":")) < n {
		return ti.RunnableTest{}, fmt.Errorf("rule does not follow the default format: %s", r)
	}
	// fullPkg = package.class
	fullPkg := strings.Split(r, ":")[1]
	for _, s := range bazelRuleSepList {
		fullPkg = strings.Replace(fullPkg, s, ".", -1)
	}
	pkgList := strings.Split(fullPkg, ".")
	if len(pkgList) < n {
		return ti.RunnableTest{}, fmt.Errorf("rule does not follow the default format: %s", r)
	}
	cls := pkgList[len(pkgList)-1]
	pkg := strings.TrimSuffix(fullPkg, "."+cls)
	test := ti.RunnableTest{Pkg: pkg, Class: cls}
	test.Autodetect.Rule = r
	return test, nil
}

func getSourceArg(changedFiles []ti.File) (string) {
	sourceString := ""
	for _, file := range changedFiles {
		if(isJavaTestFile(file.Name)) {
			sourceString = sourceString + fmt.Sprintf("-s%s ", file.Name)
		}
	}
	return sourceString
}

func isJavaTestFile(filePath string) bool {
	fileExt := filepath.Ext(filePath)
	fileName := filepath.Base(filePath)

	// Check if the file extension is ".java" and the file name ends with "Test"
	return strings.ToLower(fileExt) == ".java" && strings.HasSuffix(fileName, "Test.java")
}

func GetJavaStaticCmd(ctx context.Context, userArgs, workspace, outDir, agentInstallDir string, changedFiles []ti.File) (string, error) {
	sourceString := getSourceArg(changedFiles)
	if sourceString == "" {
		return "", nil
	}
	javaStaticAgentPath := filepath.Join(agentInstallDir, JavaStaticAgentJar)
	outPath := filepath.Join(outDir, "static_callgraph.json")
	staticCmd := fmt.Sprintf("java -jar %s -o%s %s -f%s", javaStaticAgentPath, outPath, sourceString, workspace)

	return staticCmd, nil
}

