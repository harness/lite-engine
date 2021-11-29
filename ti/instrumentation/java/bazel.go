package java

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/harness/lite-engine/internal/filesystem"
	"github.com/harness/lite-engine/ti"
	"github.com/sirupsen/logrus"
)

var (
	bazelCmd = "bazel"
)

type bazelRunner struct {
	fs  filesystem.FileSystem
	log *logrus.Logger
}

func NewBazelRunner(log *logrus.Logger, fs filesystem.FileSystem) *bazelRunner { // nolint:revive
	return &bazelRunner{
		fs:  fs,
		log: log,
	}
}

func (b *bazelRunner) AutoDetectPackages(workspace string) ([]string, error) {
	return DetectPkgs(workspace, b.log, b.fs)
}

func (b *bazelRunner) GetCmd(ctx context.Context, tests []ti.RunnableTest, userArgs, agentConfigPath string, // nolint:funlen,gocyclo
	ignoreInstr, runAll bool) (string, error) {
	if ignoreInstr {
		return fmt.Sprintf("%s %s //...", bazelCmd, userArgs), nil
	}

	agentArg := fmt.Sprintf(javaAgentArg, agentConfigPath)
	instrArg := fmt.Sprintf("--define=HARNESS_ARGS=%s", agentArg)
	defaultCmd := fmt.Sprintf("%s %s %s //...", bazelCmd, userArgs, instrArg) // run all the tests

	if runAll {
		// Run all the tests
		return defaultCmd, nil
	}
	if len(tests) == 0 {
		return "echo \"Skipping test run, received no tests to execute\"", nil // nolint:goconst
	}
	// Use only unique classes
	pkgs := []string{}
	clss := []string{}
	set := make(map[string]interface{})
	ut := []string{}
	for _, t := range tests {
		if _, ok := set[t.Class]; ok {
			// The class has already been added
			continue
		}
		set[t.Class] = struct{}{}
		ut = append(ut, t.Class) // nolint:staticcheck
		pkgs = append(pkgs, t.Pkg)
		clss = append(clss, t.Class)
	}
	rulesM := make(map[string]struct{})
	rules := []string{} // List of unique bazel rules to be executed
	for i := 0; i < len(pkgs); i++ {
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
	return fmt.Sprintf("%s %s %s %s", bazelCmd, userArgs, instrArg, testList), nil
}
