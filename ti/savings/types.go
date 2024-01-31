package savings

import "github.com/harness/ti-client/types"

type CacheType int32

const (
	GRADLE CacheType = 1
)

type Overview struct {
	CacheType
	CacheState         types.IntelligenceExecutionState
	BuildTimeMs        int64 // Wall-clock
	UploadOverheadMs   int64 // CPU time
	DownloadOverheadMs int64 // CPU time
}
