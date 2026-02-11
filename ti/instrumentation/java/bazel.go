// Copyright 2022 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package java

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/harness/lite-engine/internal/filesystem"
	"github.com/harness/lite-engine/ti/instrumentation/common"
	ti "github.com/harness/ti-client/types"
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

	c1 := fmt.Sprintf("cd %s; %s query 'kind(java.*, tests(//...))'", workspace, bazelCmd)  // bazel query 'kind(java.*, tests(//...))'
	c2 := fmt.Sprintf("cd %s; %s query 'kind(scala.*, tests(//...))'", workspace, bazelCmd) // bazel query 'kind(scala.*, tests(//...))'
	c3 := fmt.Sprintf("cd %s; %s query 'kind(kt.*, tests(//...))'", workspace, bazelCmd)    // bazel query 'kind(kt.*, tests(//...))'
	for _, c := range []string{c1, c2, c3} {
		cmdArgs := []string{"-c", c}
		resp, err := execCmdCtx(ctx, "sh", cmdArgs...).Output()
		if err != nil {
			b.log.Errorln("Got an error while querying bazel", err)
			return tests, err
		}
		// Convert rules to RunnableTest list
		for _, r := range strings.Split(string(resp), "\n") {
			test, err := parseBazelTestRule(r)
			if err != nil {
				b.log.Errorf("Error parsing bazel test rule: %s", err)
				continue
			}
			tests = append(tests, test)
		}
	}
	return tests, nil
}

func (b *bazelRunner) GetTestGlobs() (testGlobs, excludeGlobs []string) {
	return make([]string, 0), make([]string, 0)
}

func (b *bazelRunner) ReadPackages(workspace string, files []ti.File) []ti.File {
	return ReadPkgs(b.log, b.fs, workspace, files)
}

func getBazelTestRules(ctx context.Context, log *logrus.Logger, tests []ti.RunnableTest, workspace string) []ti.RunnableTest {
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
	c := fmt.Sprintf("cd %s; %s query 'attr(name, %q, //...)'", workspace, bazelCmd, queryString)
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
	workspace, agentConfigPath, agentInstallDir string, ignoreInstr, runAll bool, runnerArgs common.RunnerArgs) (string, error) {
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
	if len(tests) == 0 && len(runnerArgs.ModuleList) == 0 {
		return SkipTestRunMsg, nil
	}
	// Populate the test rules in tests
	tests = getBazelTestRules(ctx, b.log, tests, workspace)

	// Use only unique classes
	rules := make([]string, 0) // List of unique bazel rules to be executed
	rulesSet := make(map[string]bool)
	classSet := make(map[string]bool)

	// Add module test targets to rules, and filter out rules falling under these modules
	for _, module := range runnerArgs.ModuleList {
		if !b.moduleContainsTestRules(ctx, workspace, module) {
			b.log.Infof("Ignoring module %s since no test rules were found", module)
			continue
		}
		moduleRule := fmt.Sprintf("//%s/...", module)
		rules = append(rules, moduleRule)
		rulesSet[moduleRule] = true
	}

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
				testModule := getModuleFromRule(rule)
				if _, ok := rulesSet[testModule]; ok {
					continue
				}
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
		pathCmd := fmt.Sprintf(`cd %s; find . -path '*%s/%s*' | sed -e "s/^\.\///g"`, workspace, strings.Replace(pkg, ".", "/", -1), cls)
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
			// Get the full name using bazel query
			c := fmt.Sprintf("cd %s; %s query %s", workspace, bazelCmd, p)
			cmdArgs = []string{"-c", c}
			fullname, err2 := exec.CommandContext(ctx, "sh", cmdArgs...).Output()
			if err2 != nil || len(fullname) == 0 {
				b.log.WithError(err2).Errorln(fmt.Sprintf("could not find fullname for path %s with err: %s", p, err2))
				continue
			}
			// Get rule regex
			re := regexp.MustCompile(":.*")
			fullnameStr := strings.TrimSuffix(string(fullname), "\n")
			fullnameSubStr := re.ReplaceAllString(fullnameStr, ":*")
			// Get the test rule
			c = fmt.Sprintf("cd %s; %s query \"attr('srcs', %s, %s)\" --output=label_kind | grep 'java_test rule'",
				workspace, bazelCmd, fullnameStr, fullnameSubStr)
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
				testModuleR := getModuleFromRule(r)
				if _, ok := rulesSet[testModuleR]; ok {
					continue
				}
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

// parse module name from rule,
// eg - //332-ci-manager/app:src/test/java/io/harness/app/impl/CIManagerServiceTestModule.java, gives op -> //332-ci-manager/...
func getModuleFromRule(rule string) string {
	splitRule := strings.Split(strings.TrimPrefix(rule, "//"), ":")
	if len(splitRule) != 0 {
		if strings.Contains(splitRule[0], "/") {
			splitModule := strings.Split(splitRule[0], "/")
			return fmt.Sprintf("//%s/...", splitModule[0])
		}
		return fmt.Sprintf("//%s/...", splitRule[0])
	}
	return ""
}

func (b *bazelRunner) moduleContainsTestRules(ctx context.Context, workspace, module string) bool {
	c := fmt.Sprintf("cd %s; %s query 'kind(.*, tests(//%s/...))'", workspace, bazelCmd, module)
	cmdArgs := []string{"-c", c}
	resp, err := execCmdCtx(ctx, "sh", cmdArgs...).Output()
	if err != nil {
		b.log.Errorf("Got an error while querying bazel for module test rules: %s", err)
		return false
	}
	// Check if a valid test rule is found
	for _, r := range strings.Split(string(resp), "\n") {
		_, err = parseBazelTestRule(r)
		if err != nil {
			b.log.Errorf("Error parsing bazel test rule for module %s: %s", module, err)
			continue
		}
		// found a valid test rule
		return true
	}
	return false
}
