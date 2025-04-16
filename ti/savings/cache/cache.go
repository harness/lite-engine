package cache

import (
	"github.com/harness/lite-engine/ti/savings/cache/gradle"
	"github.com/harness/lite-engine/ti/savings/cache/maven"
	"github.com/harness/ti-client/types"
	gradleTypes "github.com/harness/ti-client/types/cache/gradle"
	mavenTypes "github.com/harness/ti-client/types/cache/maven"
	"github.com/sirupsen/logrus"
)

func ParseCacheSavings(workspace string, log *logrus.Logger) (types.IntelligenceExecutionState, int, types.SavingsRequest, error) {
	savingsRequest := types.SavingsRequest{}

	// TODO: This assumes that savings data is only present for Gradle. Refactor when more cache options are available
	cacheState, profiles, buildTime, err := gradle.ParseSavings(workspace, log)
	if err != nil {
		log.Warnf("Gradle savings: %s", err)
	}
	savingsRequest.GradleMetrics = gradleTypes.Metrics{Profiles: profiles}
	mavenCacheState, reports, err := maven.ParseSavings(workspace, log)
	if err != nil {
		log.Warnf("Maven savings: %s", err)
	}
	if mavenCacheState == types.OPTIMIZED {
		cacheState = mavenCacheState
	}
	savingsRequest.MavenMetrics = mavenTypes.MavenMetrics{Reports: reports}
	return cacheState, buildTime, savingsRequest, nil
}
