package cache

import (
	"github.com/harness/lite-engine/ti/savings/cache/gradle"
	"github.com/harness/ti-client/types"
	gradleTypes "github.com/harness/ti-client/types/cache/gradle"
	"github.com/sirupsen/logrus"
)

func ParseCacheSavings(workspace string, log *logrus.Logger) (types.IntelligenceExecutionState, int, types.SavingsRequest, error) {
	savingsRequest := types.SavingsRequest{}

	// TODO: This assumes that savings data is only present for Gradle. Refactor when more cache options are available
	cacheState, profiles, buildTime, err := gradle.ParseSavings(workspace, log)
	if err != nil {
		return cacheState, 0, savingsRequest, err
	}
	savingsRequest.GradleMetrics = gradleTypes.Metrics{Profiles: profiles}
	return cacheState, buildTime, savingsRequest, nil
}
