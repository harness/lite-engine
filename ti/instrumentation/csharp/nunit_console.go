// Copyright 2022 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

// Supports running tests via the nunit console test runner for C#
//
// Test filtering:
//
//	nunit3-console.exe <path-to-dll> --where "class =~ FirstTest || class =~ SecondTest"
package csharp

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/harness/lite-engine/internal/filesystem"
	"github.com/harness/lite-engine/ti/instrumentation/common"
	ti "github.com/harness/ti-client/types"

	"github.com/sirupsen/logrus"
)

type nunitConsoleRunner struct {
	fs  filesystem.FileSystem
	log *logrus.Logger
}

func NewNunitConsoleRunner(log *logrus.Logger, fs filesystem.FileSystem) *nunitConsoleRunner { //nolint:revive
	return &nunitConsoleRunner{
		fs:  fs,
		log: log,
	}
}

func (b *nunitConsoleRunner) AutoDetectPackages(workspace string) ([]string, error) {
	return []string{}, nil
}

func (b *nunitConsoleRunner) AutoDetectTests(ctx context.Context, workspace string, testGlobs []string) ([]ti.RunnableTest, error) {
	tests := GetCsharpTests(workspace, testGlobs)
	return tests, nil
}

func (b *nunitConsoleRunner) ReadPackages(workspace string, files []ti.File) []ti.File {
	return files
}

func (b *nunitConsoleRunner) GetTestGlobs() (testGlobs, excludeGlobs []string) {
	return make([]string, 0), make([]string, 0)
}

func (b *nunitConsoleRunner) GetCmd(ctx context.Context, tests []ti.RunnableTest, userArgs, workspace, //nolint:gocyclo
	agentConfigPath, agentInstallDir string, ignoreInstr, runAll bool, runnerArgs common.RunnerArgs) (string, error) {
	/*
		i) Get the DLL list from the command (assume it runs at the root of the repository)
		ii) Run the injector through all the DLLs
		iii) Add test filtering

		Working command:
			. nunit3-console.exe <path-to-dll> --where "class =~ FirstTest || class =~ SecondTest"
	*/

	var cmd string
	pathToInjector := filepath.Join(agentInstallDir, "dotnet-agent", "dotnet-agent.injector.exe")

	// Unzip everything at agentInstallDir/dotnet-agent.zip
	err := common.ExtractArchive(filepath.Join(agentInstallDir, "dotnet-agent.zip"), agentInstallDir)
	if err != nil {
		b.log.WithError(err).Println("could not unarchive the dotnet agent")
		return "", err
	}

	// Run all the DLLs through the injector
	args := strings.Split(userArgs, " ")
	for _, s := range args {
		if strings.HasSuffix(s, ".dll") {
			absPath := s
			if s[0] != '~' && s[0] != '/' && s[0] != '\\' {
				if !strings.HasPrefix(s, workspace) {
					absPath = filepath.Join(workspace, s)
				}
			}
			cmd += fmt.Sprintf(". %s %s %s\n", pathToInjector, absPath, agentConfigPath)
		}
	}

	if runAll {
		if ignoreInstr {
			return userArgs, nil
		}
		return fmt.Sprintf("%s %s", cmd, userArgs), nil
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
			testStr += " || "
		}
		testStr += fmt.Sprintf("class =~ %s", t)
	}
	if ignoreInstr {
		return fmt.Sprintf("%s --where %q", userArgs, testStr), nil
	}
	return fmt.Sprintf("%s %s --where %q", cmd, userArgs, testStr), nil
}
