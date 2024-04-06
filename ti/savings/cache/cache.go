package cache

import (
	"github.com/harness/lite-engine/ti/savings/cache/gradle"
	"github.com/harness/ti-client/types"
	"github.com/sirupsen/logrus"
)

func ParseCacheSavings(workspace string, log *logrus.Logger) (types.IntelligenceExecutionState, int, error) {
	// TODO: This assumes that savings data is only present for Gradle. Refactor when more cache options are available
	cacheState, _, buildTime, err := gradle.ParseSavings(workspace, log)
	if err != nil {
		return cacheState, 0, err
	}
	return cacheState, buildTime, nil
}
