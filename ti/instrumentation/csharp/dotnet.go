// Copyright 2022 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

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
	"os"
	"path/filepath"

	"github.com/harness/lite-engine/internal/filesystem"
	"github.com/harness/lite-engine/ti/instrumentation/common"
	ti "github.com/harness/ti-client/types"

	"github.com/sirupsen/logrus"
)

var (
	dotnetCmd = "dotnet"
)

type dotnetRunner struct {
	fs  filesystem.FileSystem
	log *logrus.Logger
}

func NewDotnetRunner(log *logrus.Logger, fs filesystem.FileSystem) *dotnetRunner { //nolint:revive
	return &dotnetRunner{
		fs:  fs,
		log: log,
	}
}

func (b *dotnetRunner) AutoDetectPackages(workspace string) ([]string, error) {
	return []string{}, nil
}

func (b *dotnetRunner) AutoDetectTests(ctx context.Context, workspace string, testGlobs []string) ([]ti.RunnableTest, error) {
	tests := GetCsharpTests(workspace, testGlobs)
	return tests, nil
}

func (b *dotnetRunner) ReadPackages(workspace string, files []ti.File) []ti.File {
	return files
}

func (b *dotnetRunner) GetTestGlobs() (testGlobs, excludeGlobs []string) {
	return make([]string, 0), make([]string, 0)
}

func (b *dotnetRunner) GetCmd(ctx context.Context, tests []ti.RunnableTest, userArgs, workspace, agentConfigPath,
	agentInstallDir string, ignoreInstr, runAll bool, runnerArgs common.RunnerArgs) (string, error) {
	// Move config.ini to Config.yaml manually for now. Later, we will use the same format for both
	// agentInstallDir should have the zip file
	/*
		Steps:
			 i)   dotnet build in the project (to be done by the customer)
			 ii)  Run agentConfigPath/injector.exe with bin/Debug/net48/ProjectName.dll and config yaml
			 iii)  Return dotnet test --no-build with args and test selection
	*/

	// This needs to be moved to the UI and made configurable: [CI-3167]
	pathToDLL := os.Getenv("PATH_TO_DLL")
	if pathToDLL == "" {
		return "", errors.New("PATH_TO_DLL env variable needs to be set")
	}

	// Unzip everything at agentInstallDir/dotnet-agent.zip
	err := common.ExtractArchive(filepath.Join(agentInstallDir, "dotnet-agent.zip"), agentInstallDir)
	if err != nil {
		b.log.WithError(err).Println("could not unarchive the dotnet agent")
		return "", err
	}

	absPath := filepath.Join(workspace, pathToDLL)
	pathToInjector := filepath.Join(agentInstallDir, "dotnet-agent", "dotnet-agent.injector.exe")

	cmd := fmt.Sprintf(". %s %s %s\n", pathToInjector, absPath, agentConfigPath)

	if runAll {
		if ignoreInstr {
			return fmt.Sprintf("%s %s", dotnetCmd, userArgs), nil
		}
		return fmt.Sprintf("%s %s test %s --no-build", cmd, dotnetCmd, userArgs), nil
	}

	if len(tests) == 0 {
		return "echo \"Skipping test run, received no tests to execute\"", nil
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

	if ignoreInstr {
		return fmt.Sprintf("%s test --filter %q %s --no-build", dotnetCmd, testStr, userArgs), nil
	}
	return fmt.Sprintf("%s %s test --filter %q %s --no-build", cmd, dotnetCmd, testStr, userArgs), nil // Add instrumentation here
}
