// Copyright 2022 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package java

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sirupsen/logrus"

	"github.com/harness/lite-engine/internal/filesystem"
	"github.com/harness/lite-engine/ti"
)

var (
	gradleWrapperCmd = "./gradlew"
	gradleCmd        = "gradle"
)

type gradleRunner struct {
	fs  filesystem.FileSystem
	log *logrus.Logger
}

func NewGradleRunner(log *logrus.Logger, fs filesystem.FileSystem) *gradleRunner { //nolint:revive
	return &gradleRunner{
		fs:  fs,
		log: log,
	}
}

func (g *gradleRunner) AutoDetectPackages(workspace string) ([]string, error) {
	return DetectPkgs(workspace, g.log, g.fs)
}

/*
The following needs to be added to a build.gradle to make it compatible with test intelligence:
// This adds HARNESS_JAVA_AGENT to the testing command if it's provided through the command line.
// Local builds will still remain same as it only adds if the parameter is provided.

	tasks.withType(Test) {
	  if(System.getProperty("HARNESS_JAVA_AGENT")) {
	    jvmArgs += [System.getProperty("HARNESS_JAVA_AGENT")]
	  }
	}

// This makes sure that any test tasks for subprojects don't fail in case the test filter does not match
// with any tests. This is needed since we want to search for a filter in all subprojects without failing if
// the filter does not match with any of the subprojects.

	gradle.projectsEvaluated {
	  tasks.withType(Test) {
	    filter {
	      setFailOnNoMatchingTests(false)
	    }
	  }
	}
*/
func (g *gradleRunner) GetCmd(ctx context.Context, tests []ti.RunnableTest, userArgs, workspace,
	agentConfigPath, agentInstallDir string, ignoreInstr, runAll bool) (string, error) {
	// Check if gradlew exists. If not, fallback to gradle
	gc := gradleWrapperCmd
	_, err := g.fs.Stat("gradlew")
	if errors.Is(err, os.ErrNotExist) {
		gc = gradleCmd
	}

	// If instrumentation needs to be ignored, we run all the tests without adding the agent config
	if ignoreInstr {
		return strings.TrimSpace(fmt.Sprintf("%s %s", gc, userArgs)), nil
	}

	var orCmd string

	if strings.Contains(userArgs, "||") {
		// args = "test || orCond1 || orCond2" gets split as:
		// [test, orCond1 || orCond2]
		s := strings.SplitN(userArgs, "||", 2) //nolint:gomnd
		orCmd = s[1]
		userArgs = s[0]
	}
	userArgs = strings.TrimSpace(userArgs)
	if orCmd != "" {
		orCmd = "|| " + strings.TrimSpace(orCmd)
	}

	javaAgentPath := filepath.Join(agentInstallDir, JavaAgentJar)
	agentArg := fmt.Sprintf(AgentArg, javaAgentPath, agentConfigPath)
	if runAll {
		// Run all the tests
		return strings.TrimSpace(fmt.Sprintf("%s %s -DHARNESS_JAVA_AGENT=%s %s", gc, userArgs, agentArg, orCmd)), nil
	}
	if len(tests) == 0 {
		return "echo \"Skipping test run, received no tests to execute\"", nil
	}
	// Use only unique <package, class> tuples
	set := make(map[ti.RunnableTest]interface{})
	var testStr string
	for _, t := range tests {
		w := ti.RunnableTest{Pkg: t.Pkg, Class: t.Class}
		if _, ok := set[w]; ok {
			// The test has already been added
			continue
		}
		set[w] = struct{}{}
		testStr = testStr + " --tests " + fmt.Sprintf("\"%s.%s\"", t.Pkg, t.Class)
	}

	return strings.TrimSpace(fmt.Sprintf("%s %s -DHARNESS_JAVA_AGENT=%s%s %s", gc, userArgs, agentArg, testStr, orCmd)), nil
}
