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
	"github.com/harness/lite-engine/ti/instrumentation/common"
	ti "github.com/harness/ti-client/types"
	"github.com/sirupsen/logrus"
)

const (
	AgentArg     = "-javaagent:%s=%s"
	JavaAgentJar = "java-agent.jar"

	// SkipTestRunMsg is the echo command returned when no tests need to be executed.
	SkipTestRunMsg = `echo "Skipping test run, received no tests to execute"`
)

const (
	JAVA_SRC_PATH      = "src/main/java/"      //nolint:revive,staticcheck
	JAVA_TEST_PATH     = "src/test/java/"      //nolint:revive,staticcheck
	JAVA_RESOURCE_PATH = "src/test/resources/" //nolint:revive,staticcheck
	SCALA_TEST_PATH    = "src/test/scala/"     //nolint:revive,staticcheck
	KOTLIN_TEST_PATH   = "src/test/kotlin/"    //nolint:revive,staticcheck
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

// ReadJavaPkg reads the package from the input java file
func ReadJavaPkg(log *logrus.Logger, fs filesystem.FileSystem, f string, excludeList []string, packageLen int) (string, error) { //nolint:gocyclo
	// TODO: (Vistaar)
	// This doesn't handle some special cases right now such as when there is a package
	// present in a multiline comment with multiple opening and closing comments.
	// We will require to read all the lines together to handle this.
	result := ""
	absPath, err := filepath.Abs(f)
	if !strings.HasSuffix(absPath, ".java") && !strings.HasSuffix(absPath, ".scala") && !strings.HasSuffix(absPath, ".kt") {
		return result, nil
	}
	if err != nil {
		log.Errorf("Failed to get absolute path for %s with error: %s", f, err)
		return "", err
	}
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
				for _, exclude := range excludeList {
					if strings.HasPrefix(pkg, exclude) {
						log.Infof("Found package: %s having same package prefix as: %s. Excluding this package from the list...", pkg, exclude)
						return nil
					}
				}
				pkg = tokens[0]
				if packageLen == -1 {
					// Read full package name
					for i, token := range tokens {
						if i == 0 {
							continue
						}
						pkg = pkg + "." + strings.TrimSpace(token)
					}
					result = pkg
					return nil
				}
				for i := 1; i < packageLen && i < len(tokens); i++ {
					pkg = pkg + "." + strings.TrimSpace(tokens[i])
				}
				if pkg == "" {
					continue
				}
				result = pkg
				return nil
			}
		}
		if err = scanner.Err(); err != nil {
			log.Errorf("Failed to scan the file %s with error: %s", f, err)
			return err
		}
		return nil
	})
	if err != nil {
		log.Errorf("Failed to auto detect java package for %s with error: %s", f, err)
	}
	return result, err
}

// ReadPkgs reads and populates java packages for all input files
func ReadPkgs(log *logrus.Logger, fs filesystem.FileSystem, workspace string, files []ti.File) []ti.File {
	for i, file := range files {
		if file.Status != ti.FileDeleted {
			fileName := fmt.Sprintf("%s/%s", workspace, file.Name)
			pkg, err := ReadJavaPkg(log, fs, fileName, make([]string, 0), -1)
			if err != nil {
				log.WithError(err).Errorln("something went wrong when parsing package, using file path as package")
			}
			files[i].Package = pkg
		}
	}
	return files
}

// DetectPkgs detects java packages by reading all the files and parsing their package names
func DetectPkgs(workspace string, log *logrus.Logger, fs filesystem.FileSystem) ([]string, error) {
	plist := make([]string, 0)
	excludeList := []string{"com.google"} // exclude any instances of these packages from the package list
	packageLen := 2                       // length of package to be auto-detected (io.harness for io.harness.ci.execution)

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
		pkg, err := ReadJavaPkg(log, fs, f, excludeList, packageLen)
		if err != nil || pkg == "" {
			continue
		}
		if _, ok := m[pkg]; !ok {
			plist = append(plist, pkg)
			m[pkg] = struct{}{}
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

func AutoDetectTests(ctx context.Context, workspace string, testGlobs []string) ([]ti.RunnableTest, error) {
	tests := make([]ti.RunnableTest, 0)
	javaTests := GetJavaTests(workspace, testGlobs)
	scalaTests := GetScalaTests(workspace, testGlobs)
	kotlinTests := GetKotlinTests(workspace, testGlobs)

	tests = append(tests, javaTests...)
	tests = append(tests, scalaTests...)
	tests = append(tests, kotlinTests...)
	return tests, nil
}
