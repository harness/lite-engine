// Copyright 2022 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package java

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/harness/lite-engine/internal/filesystem"
	"github.com/harness/lite-engine/ti"
	"github.com/sirupsen/logrus"
)

var (
	bazelCmd         = "bazel"
	bazelRuleSepList = []string{".", "/"}
	execCmdCtx       = exec.CommandContext
)

type bazelRunner struct {
	fs  filesystem.FileSystem
	log *logrus.Logger
}

func NewBazelRunner(log *logrus.Logger, fs filesystem.FileSystem) *bazelRunner { //nolint:revive
	return &bazelRunner{
		fs:  fs,
		log: log,
	}
}

func (b *bazelRunner) AutoDetectPackages(workspace string) ([]string, error) {
	return DetectPkgs(workspace, b.log, b.fs)
}

// AutoDetectTests parses all the Java test rules from bazel query to RunnableTest
func (b *bazelRunner) AutoDetectTests(ctx context.Context, workspace string) ([]ti.RunnableTest, error) {
	tests := make([]ti.RunnableTest, 0)

	// bazel query 'kind(java.*, tests(//...))'
	c := fmt.Sprintf("%s query 'kind(java.*, tests(//...))'", bazelCmd)
	cmdArgs := []string{"-c", c}
	resp, err := execCmdCtx(ctx, "sh", cmdArgs...).Output()
	if err != nil {
		b.log.Errorln("Got an error while querying bazel", err)
		return tests, err
	}
	// Convert rules to RunnableTest list
	var test ti.RunnableTest
	for _, r := range strings.Split(string(resp), "\n") {
		// r = //module:package.class
		if r == "" {
			continue
		}
		n := 2
		if !strings.Contains(r, ":") || len(strings.Split(r, ":")) < n {
			b.log.Errorln(fmt.Sprintf("Rule does not follow the default format: %s", r))
			continue
		}
		// fullPkg = package.class
		fullPkg := strings.Split(r, ":")[1]
		for _, s := range bazelRuleSepList {
			fullPkg = strings.Replace(fullPkg, s, ".", -1)
		}
		pkgList := strings.Split(fullPkg, ".")
		if len(pkgList) < n {
			b.log.Errorln(fmt.Sprintf("Rule does not follow the default format: %s", r))
			continue
		}
		cls := pkgList[len(pkgList)-1]
		pkg := strings.TrimSuffix(fullPkg, "."+cls)
		test = ti.RunnableTest{Pkg: pkg, Class: cls}
		test.Autodetect.Rule = r
		tests = append(tests, test)
	}
	return tests, nil
}

func (b *bazelRunner) GetCmd(ctx context.Context, tests []ti.RunnableTest, userArgs, //nolint:funlen,gocyclo
	workspace, agentConfigPath, agentInstallDir string, ignoreInstr, runAll bool) (string, error) {
	// Agent arg
	javaAgentPath := filepath.Join(agentInstallDir, JavaAgentJar)
	agentArg := fmt.Sprintf(AgentArg, javaAgentPath, agentConfigPath)
	instrArg := fmt.Sprintf("--define=HARNESS_ARGS=%s", agentArg)
	defaultCmd := fmt.Sprintf("%s %s %s //...", bazelCmd, userArgs, instrArg) // run all the tests

	// Run all the tests
	if runAll {
		if ignoreInstr {
			return fmt.Sprintf("%s %s //...", bazelCmd, userArgs), nil
		}
		return defaultCmd, nil
	}
	if len(tests) == 0 {
		return "echo \"Skipping test run, received no tests to execute\"", nil //nolint:goconst
	}

	// Use only unique classes
	pkgs := []string{}
	clss := []string{}
	ut := []string{}
	rls := []string{}
	for _, t := range tests {
		ut = append(ut, t.Class) //nolint:staticcheck
		pkgs = append(pkgs, t.Pkg)
		clss = append(clss, t.Class)
		rls = append(rls, t.Autodetect.Rule)
	}
	rulesM := make(map[string]struct{})
	rules := []string{} // List of unique bazel rules to be executed
	classSet := make(map[string]interface{})
	for i := 0; i < len(pkgs); i++ {
		// If the rule is present in the test, use it and skip querying bazel to get the rule
		if rls[i] != "" {
			rules = append(rules, rls[i])
			continue
		}
		if _, ok := classSet[clss[i]]; ok {
			// The class has already been queried
			continue
		}
		classSet[clss[i]] = struct{}{}
		c := fmt.Sprintf("%s query 'attr(name, %s.%s, //...)'", bazelCmd, pkgs[i], clss[i])
		cmdArgs := []string{"-c", c}
		resp, err := exec.CommandContext(ctx, "sh", cmdArgs...).Output()
		if err != nil || len(resp) == 0 {
			b.log.WithError(err).WithField("index", i).WithField("command", c).
				Errorln(fmt.Sprintf("could not find an appropriate rule for pkgs %s and class %s", pkgs[i], clss[i]))
			// Hack to get bazel rules for portal
			// TODO: figure out how to generically get rules to be executed from a package and a class
			// Example commands:
			//     find . -path "*pkg.class" -> can have multiple tests (eg helper/base tests)
			//     export fullname=$(bazelisk query path.java)
			//     bazelisk query "attr('srcs', $fullname, ${fullname//:*/}:*)" --output=label_kind | grep "java_test rule"

			// Get list of paths for the tests
			pathCmd := fmt.Sprintf(`find . -path '*%s/%s*' | sed -e "s/^\.\///g"`, strings.Replace(pkgs[i], ".", "/", -1), clss[i])
			cmdArgs = []string{"-c", pathCmd}
			pathResp, pathErr := exec.CommandContext(ctx, "sh", cmdArgs...).Output()
			if pathErr != nil {
				b.log.WithError(pathErr).Errorln(fmt.Sprintf("could not find path for pkgs %s and class %s", pkgs[i], clss[i]))
				continue
			}
			// Iterate over the paths and try to find the relevant rules
			for _, p := range strings.Split(string(pathResp), "\n") {
				p = strings.TrimSpace(p)
				if p == "" || !strings.Contains(p, "src/test") {
					continue
				}
				c = fmt.Sprintf("export fullname=$(%s query %s)\n"+
					"%s query \"attr('srcs', $fullname, ${fullname//:*/}:*)\" --output=label_kind | grep 'java_test rule'",
					bazelCmd, p, bazelCmd)
				cmdArgs = []string{"-c", c}
				resp2, err2 := exec.CommandContext(ctx, "sh", cmdArgs...).Output()
				if err2 != nil || len(resp2) == 0 {
					b.log.WithError(err2).Errorln(fmt.Sprintf("could not find an appropriate rule in failback for path %s", p))
					continue
				}
				t := strings.Fields(string(resp2))
				resp = []byte(t[2])
				r := strings.TrimSuffix(string(resp), "\n")
				if _, ok := rulesM[r]; !ok {
					rules = append(rules, r)
					rulesM[r] = struct{}{}
				}
			}
		} else {
			r := strings.TrimSuffix(string(resp), "\n")
			if _, ok := rulesM[r]; !ok {
				rules = append(rules, r)
				rulesM[r] = struct{}{}
			}
		}
	}
	if len(rules) == 0 {
		return "echo \"Could not find any relevant test rules. Skipping the run\"", nil
	}
	testList := strings.Join(rules, " ")
	if ignoreInstr {
		return fmt.Sprintf("%s %s %s", bazelCmd, userArgs, testList), nil
	}
	return fmt.Sprintf("%s %s %s %s", bazelCmd, userArgs, instrArg, testList), nil
}
