// Package csharp allows any C# application that can run through the dotnet CLI
// should be able to use this to perform test intelligence.
//
// Test filtering:
// dotnet test --filter "FullyQualifiedName~Namespace.Class|FullyQualifiedName~Namespace2.Class2..."
package csharp

import (
	"context"
	"errors"
	"fmt"

	"github.com/harness/lite-engine/internal/filesystem"
	"github.com/harness/lite-engine/ti"

	"github.com/sirupsen/logrus"
)

var (
	dotnetCmd = "dotnet"
)

type dotnetRunner struct {
	fs  filesystem.FileSystem
	log *logrus.Logger
}

func NewDotnetRunner(log *logrus.Logger, fs filesystem.FileSystem) *dotnetRunner { // nolint:revive
	return &dotnetRunner{
		fs:  fs,
		log: log,
	}
}

func (b *dotnetRunner) AutoDetectPackages(workspace string) ([]string, error) {
	return []string{}, errors.New("not implemented")
}

func (b *dotnetRunner) GetCmd(ctx context.Context, tests []ti.RunnableTest, userArgs, agentConfigPath, agentInstallDir string, ignoreInstr, runAll bool) (string, error) {
	if ignoreInstr {
		return fmt.Sprintf("%s %s", dotnetCmd, userArgs), nil
	}

	// Create instrumented command here (TODO: Need to figure out how to instrument)
	if runAll {
		return fmt.Sprintf("%s %s", dotnetCmd, userArgs), nil // Add instrumentation here
	}

	// Need to handle this for Windows as well
	if len(tests) == 0 {
		return fmt.Sprintf("echo \"Skipping test run, received no tests to execute\""), nil
	}

	// Use only unique <pkg, class> tuples (pkg is same as namespace for .Net)
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
	var testStr string
	for idx, t := range ut {
		if idx != 0 {
			testStr += "|"
		}
		testStr += "FullyQualifiedName~" + t
	}

	return fmt.Sprintf("%s %s --filter \"%s\"", dotnetCmd, userArgs, testStr), nil // Add instrumentation here
}
