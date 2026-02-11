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
	"github.com/harness/lite-engine/ti/instrumentation/common"
	ti "github.com/harness/ti-client/types"
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

func (g *gradleRunner) AutoDetectTests(ctx context.Context, workspace string, testGlobs []string) ([]ti.RunnableTest, error) {
	tests := make([]ti.RunnableTest, 0)
	javaTests := GetJavaTests(workspace, testGlobs)
	scalaTests := GetScalaTests(workspace, testGlobs)
	kotlinTests := GetKotlinTests(workspace, testGlobs)

	tests = append(tests, javaTests...)
	tests = append(tests, scalaTests...)
	tests = append(tests, kotlinTests...)
	return tests, nil
}

func (g *gradleRunner) ReadPackages(workspace string, files []ti.File) []ti.File {
	return ReadPkgs(g.log, g.fs, workspace, files)
}

func (g *gradleRunner) GetTestGlobs() (testGlobs, excludeGlobs []string) {
	return make([]string, 0), make([]string, 0)
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
	agentConfigPath, agentInstallDir string, ignoreInstr, runAll bool, runnerArgs common.RunnerArgs) (string, error) {
	// Check if gradlew exists. If not, fallback to gradle
	gradlewPath := filepath.Join(workspace, "gradlew")

	gc := gradleWrapperCmd
	_, err := g.fs.Stat(gradlewPath)
	if errors.Is(err, os.ErrNotExist) {
		gc = gradleCmd
	}

	// If instrumentation needs to be ignored, we run all the tests without adding the agent config

	var orCmd string

	if strings.Contains(userArgs, "||") {
		// args = "test || orCond1 || orCond2" gets split as:
		// [test, orCond1 || orCond2]
		s := strings.SplitN(userArgs, "||", 2) //nolint:mnd
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
		if ignoreInstr {
			return strings.TrimSpace(fmt.Sprintf("%s %s", gc, userArgs)), nil
		}
		return strings.TrimSpace(fmt.Sprintf("%s %s -DHARNESS_JAVA_AGENT=%s %s", gc, userArgs, agentArg, orCmd)), nil
	}
	if len(tests) == 0 {
		return SkipTestRunMsg, nil
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

	if ignoreInstr {
		return strings.TrimSpace(fmt.Sprintf("%s %s%s %s", gc, userArgs, testStr, orCmd)), nil
	}
	return strings.TrimSpace(fmt.Sprintf("%s %s -DHARNESS_JAVA_AGENT=%s%s %s", gc, userArgs, agentArg, testStr, orCmd)), nil
}
