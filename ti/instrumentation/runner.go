// TestRunner provides the test command and java agent config
// which is needed to run test intelligence.
package instrumentation

import (
	"context"

	"github.com/harness/lite-engine/ti"
)

//go:generate mockgen -source runner.go -package=instrumentation -destination mocks/runner_mock.go TestRunner
type TestRunner interface {
	// Get the command which needs to be executed to run only the specified tests.
	// tests: list of selected tests which need to be executed
	// agentConfigPath: path to the java agent config. This needs to be added to the
	// command if instrumentation is required.
	// ignoreInstr: instrumentation might not be required in some cases like manual executions
	// runAll: if there was any issue in figuring out which tests to run, this parameter is set as true
	GetCmd(ctx context.Context, tests []ti.RunnableTest, userArgs, agentConfigPath string, ignoreInstr, runAll bool) (string, error)

	// Auto detect the list of packages to be instrumented.
	// Return an error if we could not detect or if it's unimplemented.
	AutoDetectPackages(workspace string) ([]string, error)
}
