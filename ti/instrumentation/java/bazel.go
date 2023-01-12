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
func (b *bazelRunner) AutoDetectTests(ctx context.Context, workspace string, testGlobs []string) ([]ti.RunnableTest, error) {
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
		test, err = parseBazelTestRule(r)
		if err != nil {
			b.log.Errorf(fmt.Sprintf("Error parsing bazel test rule: %s", err))
			continue
		}
		tests = append(tests, test)
	}
	return tests, nil
}

func getBazelTestRules(ctx context.Context, log *logrus.Logger, tests []ti.RunnableTest) []ti.RunnableTest {
	var testList []ti.RunnableTest
	var testStrings []string

	// Convert list of tests to "pkg1.cls1|pkg2.cls2|pkg3.cls3"
	testSet := map[string]bool{}
	for _, test := range tests {
		if test.Autodetect.Rule != "" {
			continue
		}
		testString := fmt.Sprintf("%s.%s", test.Pkg, test.Class)
		if _, ok := testSet[testString]; ok {
			continue
		}
		testSet[testString] = true
		testStrings = append(testStrings, testString)
	}
	if len(testStrings) == 0 {
		return tests
	}
	queryString := strings.Join(testStrings, "|")

	// bazel query 'attr(name, "pkg1.cls1|pkg2.cls2|pkg3.cls3", //...)'
	c := fmt.Sprintf("%s query 'attr(name, %q, //...)'", bazelCmd, queryString)
	cmdArgs := []string{"-c", c}
	resp, err := execCmdCtx(ctx, "sh", cmdArgs...).Output()
	if err != nil {
		log.Errorf("Got an error while querying bazel %s", err)
		return tests
	}
	ruleString := strings.TrimSuffix(string(resp), "\n")

	// Map: {pkg1.cls1 : //rule:pkg1.cls1}
	testRuleMap := map[string]string{}
	for _, r := range strings.Split(ruleString, "\n") {
		test, err := parseBazelTestRule(r)
		if err != nil {
			log.Errorf("Failed to parse test rule: %s", err)
			continue
		}
		testID := fmt.Sprintf("%s.%s", test.Pkg, test.Class)
		if _, ok := testRuleMap[testID]; !ok {
			testRuleMap[testID] = r
		}
	}

	// Loop over all the tests and check if we were able to find the rule
	for _, test := range tests {
		testID := fmt.Sprintf("%s.%s", test.Pkg, test.Class)
		if _, ok := testRuleMap[testID]; ok && test.Autodetect.Rule == "" {
			test.Autodetect.Rule = testRuleMap[testID]
		}
		testList = append(testList, test)
	}
	log.Infof("Running tests with bazel rules: %s", testList)
	return testList
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

	// Populate the test rules in tests
	tests = getBazelTestRules(ctx, b.log, tests)

	// Use only unique classes
	rules := make([]string, 0) // List of unique bazel rules to be executed
	rulesSet := make(map[string]bool)
	classSet := make(map[string]bool)
	for _, test := range tests {
		pkg := test.Pkg
		cls := test.Class
		rule := test.Autodetect.Rule

		// Check if class has already been queried
		testID := fmt.Sprintf("%s.%s", pkg, cls)
		if _, ok := classSet[testID]; ok {
			continue
		}
		classSet[testID] = true

		// If the rule is present in the test, use it and skip querying bazel to get the rule
		if rule != "" {
			if _, ok := rulesSet[rule]; !ok {
				rules = append(rules, rule)
				rulesSet[rule] = true
			}
			continue
		}

		b.log.Errorln(fmt.Sprintf("could not find an appropriate rule for pkgs %s and class %s", pkg, cls))
		// Hack to get bazel rules for portal
		// TODO: figure out how to generically get rules to be executed from a package and a class
		// Example commands:
		//     find . -path "*pkg.class" -> can have multiple tests (eg helper/base tests)
		//     export fullname=$(bazelisk query path.java)
		//     bazelisk query "attr('srcs', $fullname, ${fullname//:*/}:*)" --output=label_kind | grep "java_test rule"

		// Get list of paths for the tests
		pathCmd := fmt.Sprintf(`find . -path '*%s/%s*' | sed -e "s/^\.\///g"`, strings.Replace(pkg, ".", "/", -1), cls)
		cmdArgs := []string{"-c", pathCmd}
		pathResp, pathErr := exec.CommandContext(ctx, "sh", cmdArgs...).Output()
		if pathErr != nil {
			b.log.WithError(pathErr).Errorln(fmt.Sprintf("could not find path for pkgs %s and class %s", pkg, cls))
			continue
		}
		// Iterate over the paths and try to find the relevant rules
		for _, p := range strings.Split(string(pathResp), "\n") {
			p = strings.TrimSpace(p)
			if p == "" || !strings.Contains(p, "src/test") {
				continue
			}
			c := fmt.Sprintf("export fullname=$(%s query %s)\n"+
				"%s query \"attr('srcs', $fullname, ${fullname//:*/}:*)\" --output=label_kind | grep 'java_test rule'",
				bazelCmd, p, bazelCmd)
			cmdArgs = []string{"-c", c}
			resp2, err2 := exec.CommandContext(ctx, "sh", cmdArgs...).Output()
			if err2 != nil || len(resp2) == 0 {
				b.log.WithError(err2).Errorln(fmt.Sprintf("could not find an appropriate rule in failback for path %s", p))
				continue
			}
			t := strings.Fields(string(resp2))
			resp := []byte(t[2])
			r := strings.TrimSuffix(string(resp), "\n")
			if _, ok := rulesSet[r]; !ok {
				rules = append(rules, r)
				rulesSet[r] = true
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
