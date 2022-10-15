package testsplitter

const (
	ApplicationName = "split-tests"
	NumSplitsEnv    = "HARNESS_NODE_TOTAL" // Environment variable for total number of splits
	CurrentIndexEnv = "HARNESS_NODE_INDEX" // Environment variable for the current index

	SplitByFileTimeStr      = "file_timing"
	SplitByClassTimeStr     = "class_timing"
	SplitByTestcaseTimeStr  = "testcase_timing"
	SplitByTestSuiteTimeStr = "testsuite_timing"
	SplitByFileSizeStr      = "file_size"
	SplitByTestCount        = "test_count"
)
