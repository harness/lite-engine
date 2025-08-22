// Copyright 2022 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package report

import (
	"context"
	"testing"
	"time"

	"github.com/harness/lite-engine/api"
	tiCfg "github.com/harness/lite-engine/ti/config"
	"github.com/harness/ti-client/types"
	"github.com/sirupsen/logrus"
	"github.com/sirupsen/logrus/hooks/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// Mock TI client
type mockTIClient struct {
	mock.Mock
}

func (m *mockTIClient) Write(ctx context.Context, stepID, reportType string, tests []*types.TestCase) error {
	args := m.Called(ctx, stepID, reportType, tests)
	return args.Error(0)
}

func (m *mockTIClient) Summary(ctx context.Context, request types.SummaryRequest) (*types.SummaryResponse, error) {
	args := m.Called(ctx, request)
	return args.Get(0).(*types.SummaryResponse), args.Error(1)
}

// Mock TI config
type mockTIConfig struct {
	mock.Mock
	client *mockTIClient
}

func (m *mockTIConfig) GetClient() tiCfg.Client {
	return m.client
}

func (m *mockTIConfig) GetOrgID() string {
	return "test-org"
}

func (m *mockTIConfig) GetProjectID() string {
	return "test-project"
}

func (m *mockTIConfig) GetPipelineID() string {
	return "test-pipeline"
}

func (m *mockTIConfig) GetBuildID() string {
	return "test-build"
}

func (m *mockTIConfig) GetStageID() string {
	return "test-stage"
}

func TestParseAndUploadTests_EmptyPath(t *testing.T) {
	// Setup
	ctx := context.Background()
	workDir := "/workdir"
	stepID := "test-step"
	
	// Create a test logger with a hook to capture log entries
	logger, hook := test.NewNullLogger()
	logger.SetLevel(logrus.InfoLevel)
	
	// Create mock TI client and config
	mockClient := new(mockTIClient)
	mockConfig := &mockTIConfig{
		client: mockClient,
	}
	
	// Create test metadata
	testMetadata := &types.TestIntelligenceMetaData{}
	
	// Create a test report with an empty path
	testReport := api.TestReport{
		Kind: api.Junit,
		Junit: api.JunitReport{
			Paths: []string{"valid-path.xml", "", "another-valid-path.xml"},
		},
	}
	
	// Set expectations
	mockClient.On("Write", mock.Anything, stepID, "junit", mock.Anything).Return(nil)
	
	// Execute the function
	start := time.Now()
	tests, err := ParseAndUploadTests(ctx, testReport, workDir, stepID, logger, start, mockConfig, testMetadata, nil)
	
	// Assertions
	assert.NoError(t, err)
	assert.NotNil(t, tests)
	
	// Verify that the empty path was logged as a warning
	var foundWarningLog bool
	for _, entry := range hook.AllEntries() {
		if entry.Level == logrus.WarnLevel && entry.Message == "Empty path found at index 1, skipping" {
			foundWarningLog = true
			break
		}
	}
	assert.True(t, foundWarningLog, "Expected warning log for empty path not found")
	
	// Verify that the paths were properly processed (empty path skipped)
	assert.Equal(t, 2, len(testReport.Junit.Paths), "Expected 2 valid paths after processing")
	assert.Equal(t, "/workdir/valid-path.xml", testReport.Junit.Paths[0], "First path should be joined with workDir")
	assert.Equal(t, "/workdir/another-valid-path.xml", testReport.Junit.Paths[2], "Third path should be joined with workDir")
	
	// Verify mock expectations
	mockClient.AssertExpectations(t)
}
