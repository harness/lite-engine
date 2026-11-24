package telemetry

// Test fixtures shared across files.
const (
	testEndpoint  = "http://h:1"
	testPool      = "linux-amd64"
	testPoolLabel = "pool"

	// Wire-level values, spelled out here on purpose and independently of the
	// production constants: a test must fail if a production constant changes.
	wireOutcomeAttr  = "outcome"
	wireOutcomeError = "error"
	wireLogsvc       = "logsvc"
)
