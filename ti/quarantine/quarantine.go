// Copyright 2025 Harness Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package quarantine

import (
	"context"
	"fmt"
	"os"

	tiCfg "github.com/harness/lite-engine/ti/config"
	"github.com/harness/ti-client/types"
	"github.com/sirupsen/logrus"
)

const (
	// QuarantineSkipEnvVar is the environment variable/FF to enable quarantined test skip feature
	QuarantineSkipEnvVar = "CI_ENABLE_QUARANTINED_TEST_SKIP"
)

// GetQuarantinedTests fetches the list of quarantined tests from TI service using the TI client
func GetQuarantinedTests(ctx context.Context, tiConfig *tiCfg.Cfg, log *logrus.Logger) ([]types.MarkedTest, error) {
	client := tiConfig.GetClient()
	if client == nil {
		return nil, fmt.Errorf("TI client is not initialized")
	}

	resp, err := client.GetQuarantinedTests(ctx)
	if err != nil {
		log.WithError(err).Warnln("Failed to fetch quarantined tests from TI service")
		return nil, err
	}

	return resp.Tests, nil
}

// AreAllFailedTestsQuarantined checks if all failed tests are in the quarantined list
// Returns true if all failed tests are quarantined, false otherwise
func AreAllFailedTestsQuarantined(tests []*types.TestCase, quarantinedTests []types.MarkedTest, log *logrus.Logger) bool {
	if len(tests) == 0 {
		return false
	}

	// Build a set of quarantined test identifiers for efficient lookup
	// Key format: className + "::" + testName
	quarantineSet := make(map[string]bool)
	for _, qt := range quarantinedTests {
		key := fmt.Sprintf("%s::%s", qt.ClassName, qt.TestName)
		quarantineSet[key] = true
	}

	// Count failed tests and check if all are quarantined
	failedCount := 0
	quarantinedFailedCount := 0
	for _, test := range tests {
		if test.Result.Status == types.StatusFailed || test.Result.Status == types.StatusError {
			failedCount++
			key := fmt.Sprintf("%s::%s", test.ClassName, test.Name)
			if quarantineSet[key] {
				quarantinedFailedCount++
				log.Infof("Quarantined test failed: %s::%s", test.ClassName, test.Name)
			}
		}
	}

	if failedCount == 0 {
		return false
	}

	allQuarantined := failedCount == quarantinedFailedCount
	if allQuarantined {
		log.Infof("All %d failed tests are quarantined, marking step as successful", failedCount)
	} else {
		log.Infof("%d of %d failed tests are quarantined", quarantinedFailedCount, failedCount)
	}

	return allQuarantined
}

// CheckAndUpdateExitCodeForQuarantinedTests checks if all failed tests are quarantined
// and returns an updated exit code (0 if all failed tests are quarantined) and error (nil if quarantined)
func CheckAndUpdateExitCodeForQuarantinedTests(
	ctx context.Context,
	tests []*types.TestCase,
	tiConfig *tiCfg.Cfg,
	log *logrus.Logger,
	currentExitCode int,
	currentErr error,
) (int, error) {
	// Check if feature flag is enabled
	envVal := os.Getenv(QuarantineSkipEnvVar)
	if envVal != "true" {
		return currentExitCode, currentErr
	}

	log.Infof("Quarantine skip feature enabled, checking failed tests (exit_code=%d, num_tests=%d)", currentExitCode, len(tests))

	// Nothing to do if already successful
	if currentExitCode == 0 {
		return currentExitCode, currentErr
	}

	// No tests to check
	if len(tests) == 0 {
		log.Warnf("Skipping quarantine check - no test results available")
		return currentExitCode, currentErr
	}

	// Fetch quarantined tests
	log.Infof("Fetching quarantined tests from TI service (url=%s, account=%s, org=%s, project=%s, repo=%s)",
		tiConfig.GetURL(), tiConfig.GetAccountID(), tiConfig.GetOrgID(), tiConfig.GetProjectID(), tiConfig.GetRepo())
	quarantinedTests, err := GetQuarantinedTests(ctx, tiConfig, log)
	if err != nil {
		// If we can't fetch quarantined tests, keep the original exit code and error
		log.Warnf("Failed to fetch quarantined tests, keeping original exit code: %v", err)
		return currentExitCode, currentErr
	}

	log.Infof("Fetched %d quarantined tests from TI service", len(quarantinedTests))

	if len(quarantinedTests) == 0 {
		return currentExitCode, currentErr
	}

	// Check if all failed tests are quarantined
	if AreAllFailedTestsQuarantined(tests, quarantinedTests, log) {
		log.Infoln("Overriding exit code to 0 because all failed tests are quarantined")
		return 0, nil
	}

	return currentExitCode, currentErr
}
