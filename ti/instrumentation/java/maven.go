// Copyright 2022 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package java

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/harness/lite-engine/internal/filesystem"
	"github.com/harness/lite-engine/ti"
	"github.com/sirupsen/logrus"
)

var (
	mavenCmd = "mvn"
)

type mavenRunner struct {
	fs  filesystem.FileSystem
	log *logrus.Logger
}

func NewMavenRunner(log *logrus.Logger, fs filesystem.FileSystem) *mavenRunner { //nolint:revive
	return &mavenRunner{
		fs:  fs,
		log: log,
	}
}

func (m *mavenRunner) AutoDetectPackages(workspace string) ([]string, error) {
	return DetectPkgs(workspace, m.log, m.fs)
}

// AutoDetectTests parses all the Java test files and converts them to RunnableTest
func (m *mavenRunner) AutoDetectTests(ctx context.Context, workspace string) ([]ti.RunnableTest, error) {
	tests := make([]ti.RunnableTest, 0)
	files, _ := getFiles(fmt.Sprintf("%s/**/*.java", workspace))
	for _, path := range files {
		if path == "" {
			continue
		}
		node, _ := ParseJavaNode(path)
		if node.Type != NodeType_TEST {
			continue
		}
		test := ti.RunnableTest{
			Pkg:   node.Pkg,
			Class: node.Class,
		}
		tests = append(tests, test)
	}
	return tests, nil
}

func (m *mavenRunner) GetCmd(ctx context.Context, tests []ti.RunnableTest, userArgs, workspace,
	agentConfigPath, agentInstallDir string, ignoreInstr, runAll bool) (string, error) {
	// If instrumentation needs to be ignored, we run all the tests without adding the agent config
	if ignoreInstr {
		return strings.TrimSpace(fmt.Sprintf("%s %s", mavenCmd, userArgs)), nil
	}

	javaAgentPath := filepath.Join(agentInstallDir, JavaAgentJar)

	agentArg := fmt.Sprintf(AgentArg, javaAgentPath, agentConfigPath)
	instrArg := agentArg
	re := regexp.MustCompile(`(-Duser\.\S*)`)
	s := re.FindAllString(userArgs, -1)
	fmt.Println("s: ", s)
	if s != nil {
		// If user args are present, move them to instrumentation
		userArgs = re.ReplaceAllString(userArgs, "")                        // Remove from arg
		instrArg = fmt.Sprintf("\"%s %s\"", strings.Join(s, " "), agentArg) // Add to instrumentation
	}
	if !strings.HasPrefix(instrArg, "") {
		instrArg = fmt.Sprintf("%q", instrArg) // add double quotes to the instrumentation arg (needed for Windows OS)
	}
	if runAll {
		// Run all the tests
		return strings.TrimSpace(fmt.Sprintf("%s -am -DharnessArgLine=%s -DargLine=%s %s", mavenCmd, instrArg, instrArg, userArgs)), nil
	}
	if len(tests) == 0 {
		return "echo \"Skipping test run, received no tests to execute\"", nil
	}
	// Use only unique <package, class> tuples
	set := make(map[ti.RunnableTest]interface{})
	ut := []string{}
	for _, t := range tests {
		w := ti.RunnableTest{Pkg: t.Pkg, Class: t.Class}
		if _, ok := set[w]; ok {
			// The test has already been added
			continue
		}
		set[w] = struct{}{}
		if t.Pkg != "" {
			ut = append(ut, t.Pkg+"."+t.Class) // We should always have a package name. If not, use class to run
		} else {
			ut = append(ut, t.Class)
		}
	}
	testStr := strings.Join(ut, ",")
	return strings.TrimSpace(fmt.Sprintf("%s -Dtest=%s -am -DharnessArgLine=%s -DargLine=%s %s", mavenCmd, testStr, instrArg, instrArg, userArgs)), nil
}
