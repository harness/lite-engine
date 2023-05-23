// Copyright 2022 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

// TestRunner provides the test command and java agent config
// which is needed to run test intelligence.
package instrumentation

import (
	"context"

	"github.com/harness/lite-engine/ti"
)

//go:generate mockgen -source runner.go -package=instrumentation -destination mocks/runner_mock.go TestRunner
type TestRunner interface {
	// GetCmd gets the command which needs to be executed to run only the specified tests.
	// tests: list of selected tests which need to be executed
	// agentConfigPath: path to the agent config. This needs to be added to the
	// command if instrumentation is required.
	// workspace: path to the source code
	// agentInstallDir: directory where all the agent artifacts are downloaded
	// ignoreInstr: instrumentation might not be required in some cases like manual executions
	// runAll: if there was any issue in figuring out which tests to run, this parameter is set as true
	GetCmd(ctx context.Context, tests []ti.RunnableTest, userArgs, workspace, agentConfigPath, agentInstallDir string, ignoreInstr, runAll bool) (string, error)

	// AutoDetectPackages detects the list of packages to be instrumented.
	// Return an error if we could not detect or if it's unimplemented.
	AutoDetectPackages(workspace string) ([]string, error)

	// AutoDetectTests detects the list of tests in the workspace
	// Return an error if we could not detect or if it's unimplemented
	AutoDetectTests(ctx context.Context, workspace string, testGlobs []string) ([]ti.RunnableTest, error)

	GetStaticCmd(ctx context.Context, userArgs, workspace, outDir, agentInstallDir string, changedFiles []ti.File) (string, error)
}
