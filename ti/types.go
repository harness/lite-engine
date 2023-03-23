// Copyright 2022 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package ti

const (
	// StatusPassed represents a passed test.
	StatusPassed = "passed"

	// StatusSkipped represents a test case that was intentionally skipped.
	StatusSkipped = "skipped"

	// StatusFailure represents a violation of declared test expectations,
	// such as a failed assertion.
	StatusFailed = "failed"

	// StatusError represents an unexpected violation of the test itself, such as
	// an uncaught exception.
	StatusError = "error"

	// SelectSourceCode represents a selection corresponding to source code changes.
	SelectSourceCode = "source_code"

	// SelectNewTest represents a selection corresponding to a new test (eg a new test
	// introduced in the PR).
	SelectNewTest = "new_test"

	// SelectUpdatedTest represents a selection corresponding to an updated test (eg an existing
	// test which was modified).
	SelectUpdatedTest = "updated_test"

	// SelectFlakyTest represents a selection of a test because it's flaky.
	SelectFlakyTest = "flaky_test"

	// FileModified represents a modified file. Keeping it consistent with git syntax.
	FileModified = "modified"

	// FileAdded represents a file which was added in the PR.
	FileAdded = "added"

	// FileDeleted represents a file which was deleted in the PR.
	FileDeleted = "deleted"

	// AccountIDEnv represents the environment variable for Harness Account ID of the pipeline execution
	AccountIDEnv = "HARNESS_ACCOUNT_ID"

	// OrgIDEnv represents the environment variable for Organization ID of the pipeline execution
	OrgIDEnv = "HARNESS_ORG_ID"

	// ProjectIDEnv represents the environment variable for Project ID of the pipeline execution
	ProjectIDEnv = "HARNESS_PROJECT_ID"

	// PipelineIDEnv represents the environment variable for Pipeline ID of the pipeline execution
	PipelineIDEnv = "HARNESS_PIPELINE_ID"

	// TiSvcEp represents the environment variable for TI service endpoint
	TiSvcEp = "HARNESS_TI_SERVICE_ENDPOINT"

	// TiSvcToken represents the environment variable for TI service token
	TiSvcToken = "HARNESS_TI_SERVICE_TOKEN" //nolint:gosec

	// InfraEnv represents the environment variable for infra on which the pipeline is running
	InfraEnv = "HARNESS_INFRA"

	// HarnessInfra represents the environment in which the build is running
	HarnessInfra = "VM"
)

type (
	Status     string
	FileStatus string
	Selection  string

	Result struct {
		Status  Status `json:"status"`
		Message string `json:"message"`
		Type    string `json:"type"`
		Desc    string `json:"desc"`
	}

	ResponseMetadata struct {
		TotalPages    int `json:"totalPages"`
		TotalItems    int `json:"totalItems"`
		PageItemCount int `json:"pageItemCount"`
		PageSize      int `json:"pageSize"`
	}

	TestCases struct {
		Metadata ResponseMetadata `json:"data"`
		Tests    []TestCase       `json:"content"`
	}

	TestCase struct {
		Name       string `json:"name"`
		ClassName  string `json:"class_name"`
		FileName   string `json:"file_name"`
		SuiteName  string `json:"suite_name"`
		Result     Result `json:"result"`
		DurationMs int64  `json:"duration_ms"`
		SystemOut  string `json:"stdout"`
		SystemErr  string `json:"stderr"`
	}

	TestSuites struct {
		Metadata ResponseMetadata `json:"data"`
		Suites   []TestSuite      `json:"content"`
	}

	TestSummary struct {
		Name   string `json:"name"`
		Status Status `json:"status"`
	}

	SummaryResponse struct {
		TotalTests      int   `json:"total_tests"`
		FailedTests     int   `json:"failed_tests"`
		SuccessfulTests int   `json:"successful_tests"`
		SkippedTests    int   `json:"skipped_tests"`
		TimeMs          int64 `json:"duration_ms"`
	}

	StepInfo struct {
		Step  string `json:"step"`
		Stage string `json:"stage"`
	}

	TestSuite struct {
		Name         string `json:"name"`
		DurationMs   int64  `json:"duration_ms"`
		TotalTests   int    `json:"total_tests"`
		FailedTests  int    `json:"failed_tests"`
		SkippedTests int    `json:"skipped_tests"`
		PassedTests  int    `json:"passed_tests"`
		FailPct      int    `json:"fail_pct"`
	}

	// Test Intelligence specific structs

	// RunnableTest contains information about a test to run it.
	// This is different from TestCase struct which contains information
	// about a test case run. RunnableTest is used to run a test.
	RunnableTest struct {
		Pkg        string    `json:"pkg"`
		Class      string    `json:"class"`
		Method     string    `json:"method"`
		Selection  Selection `json:"selection"` // information on why a test was selected
		Autodetect struct {
			// auto-detection info depending on the runner
			Rule string `json:"rule"` // bazel
		} `json:"autodetect"`
	}

	SelectTestsResp struct {
		TotalTests    int            `json:"total_tests"`
		SelectedTests int            `json:"selected_tests"`
		NewTests      int            `json:"new_tests"`
		UpdatedTests  int            `json:"updated_tests"`
		SrcCodeTests  int            `json:"src_code_tests"`
		SelectAll     bool           `json:"select_all"` // We might choose to run all the tests
		Tests         []RunnableTest `json:"tests"`
	}

	SelectTestsReq struct {
		// If this is specified, TI service will return saying it wants to run all the tests. We want to
		// maintain stats even when all the tests are run.
		SelectAll    bool   `json:"select_all"`
		Files        []File `json:"files"`
		TargetBranch string `json:"target_branch"`
		Repo         string `json:"repo"`
		TiConfig     Config `json:"ti_config"`
	}

	SelectionDetails struct {
		New int `json:"new_tests"`
		Upd int `json:"updated_tests"`
		Src int `json:"source_code_changes"`
	}

	SelectionOverview struct {
		Total        int              `json:"total_tests"`
		Skipped      int              `json:"skipped_tests"`
		TimeSavedMs  int              `json:"time_saved_ms"`
		TimeTakenMs  int              `json:"time_taken_ms"`
		Repo         string           `json:"repo"`
		SourceBranch string           `json:"source_branch"`
		TargetBranch string           `json:"target_branch"`
		Selected     SelectionDetails `json:"selected_tests"`
	}

	GetTestTimesReq struct {
		IncludeFilename  bool `json:"include_filename"`
		IncludeTestSuite bool `json:"include_test_suite"`
		IncludeTestCase  bool `json:"include_test_case"`
		IncludeClassname bool `json:"include_classname"`
	}

	GetTestTimesResp struct {
		FileTimeMap  map[string]int `json:"file_time_map"`
		SuiteTimeMap map[string]int `json:"suite_time_map"`
		TestTimeMap  map[string]int `json:"test_time_map"`
		ClassTimeMap map[string]int `json:"class_time_map"`
	}

	File struct {
		Name   string     `json:"name"`
		Status FileStatus `json:"status"`
	}

	DownloadLink struct {
		URL     string `json:"url"`
		RelPath string `json:"rel_path"` // this is the relative path to the artifact from the base URL
	}

	// This is a yaml file which may or may not exist in the root of the source code
	// as .ticonfig. The contents of the file get deserialized into this object.
	// Sample YAML:
	// config:
	//   ignore:
	//     - README.md
	//     - config.sh
	Config struct {
		Config struct {
			Ignore []string `json:"ignore"`
		}
	}

	DiffInfo struct {
		Sha   string
		Files []File
	}

	MergePartialCgRequest struct {
		AccountID    string
		Repo         string
		TargetBranch string
		Diff         DiffInfo
	}

	// Visualization structures

	// Simplified node
	VisNode struct {
		ID int `json:"id"`

		Package string `json:"package"`
		Class   string `json:"class"`
		File    string `json:"file"`
		Type    string `json:"type"`
		Root    bool   `json:"root,omitempty"`
		// Gives information about useful nodes which might be used by UI on which nodes to center
		Important bool `json:"important"`
	}

	VisMapping struct {
		From int   `json:"from"`
		To   []int `json:"to"`
	}

	GetVgReq struct {
		AccountID    string
		Repo         string
		SourceBranch string
		TargetBranch string
		Limit        int64
		Class        string
		DiffFiles    []File
	}

	GetVgResp struct {
		Nodes []VisNode    `json:"nodes"`
		Edges []VisMapping `json:"edges"`
	}
)

func ConvertToFileStatus(s string) FileStatus {
	switch s {
	case FileModified:
		return FileModified
	case FileAdded:
		return FileAdded
	case FileDeleted:
		return FileDeleted
	}
	return FileModified
}
