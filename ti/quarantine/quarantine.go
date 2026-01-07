// Copyright 2025 Harness Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package quarantine

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	tiCfg "github.com/harness/lite-engine/ti/config"
	"github.com/harness/ti-client/types"
	"github.com/sirupsen/logrus"
)

const (
	// QuarantineSkipEnvVar is the environment variable/FF to enable quarantined test skip feature
	QuarantineSkipEnvVar = "CI_ENABLE_QUARANTINED_TEST_SKIP"
)

// GetQuarantinedTests fetches the list of quarantined tests from TI service
func GetQuarantinedTests(ctx context.Context, tiConfig *tiCfg.Cfg, log *logrus.Logger) ([]types.MarkedTest, error) {
	// Build the URL for the quarantined tests endpoint
	endpoint := fmt.Sprintf("%s/test-management/quarantined?accountId=%s&orgId=%s&projectId=%s&repo=%s",
		tiConfig.GetURL(),
		tiConfig.GetAccountID(),
		tiConfig.GetOrgID(),
		tiConfig.GetProjectID(),
		tiConfig.GetRepo(),
	)

	// Create HTTP request with timeout
	reqCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, endpoint, &bytes.Buffer{})
	if err != nil {
		log.WithError(err).Warnln("Failed to create request for quarantined tests")
		return nil, err
	}

	// Add authorization header
	req.Header.Set("X-Harness-Token", tiConfig.GetToken())
	req.Header.Set("Content-Type", "application/json")

	// Make the request
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		log.WithError(err).Warnln("Failed to fetch quarantined tests from TI service")
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Warnf("Quarantined tests endpoint returned status %d", resp.StatusCode)
		return nil, fmt.Errorf("quarantined tests endpoint returned status %d", resp.StatusCode)
	}

	// Parse the response
	var result types.MarkedTestsResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		log.WithError(err).Warnln("Failed to parse quarantined tests response")
		return nil, err
	}

	return result.Tests, nil
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
	if os.Getenv(QuarantineSkipEnvVar) != "true" {
		return currentExitCode, currentErr
	}

	// Nothing to do if already successful
	if currentExitCode == 0 {
		return currentExitCode, currentErr
	}

	// No tests to check
	if len(tests) == 0 {
		return currentExitCode, currentErr
	}

	// Fetch quarantined tests
	quarantinedTests, err := GetQuarantinedTests(ctx, tiConfig, log)
	if err != nil {
		// If we can't fetch quarantined tests, keep the original exit code and error
		return currentExitCode, currentErr
	}

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
