package cache

import (
	"encoding/json"

	"github.com/harness/lite-engine/ti/savings/cache/gradle"
	"github.com/harness/ti-client/types"
	buildcacheTypes "github.com/harness/ti-client/types/cache/buildcache"
	gradleTypes "github.com/harness/ti-client/types/cache/gradle"
	"github.com/sirupsen/logrus"
)

func createMetadataFromGradleMetrics(metrics gradleTypes.Metrics) buildcacheTypes.Metadata {
	totalTasks := 0
	cachedTasks := 0

	for _, profile := range metrics.Profiles {
		for _, project := range profile.Projects {
			for _, task := range project.Tasks {
				totalTasks++
				if task.State == "FROM-CACHE" {
					cachedTasks++
				}
			}
		}
	}

	return buildcacheTypes.Metadata{
		TotalTasks: totalTasks,
		Cached:     cachedTasks,
	}
}

func ParseCacheSavings(workspace string, log *logrus.Logger) (types.IntelligenceExecutionState, int, types.SavingsRequest, error) {
	savingsRequest := types.SavingsRequest{}

	// TODO: This assumes that savings data is only present for Gradle. Refactor when more cache options are available
	cacheState, profiles, buildTime, err := gradle.ParseSavings(workspace, log)
	if err != nil {
		return cacheState, 0, savingsRequest, err
	}
	savingsRequest.GradleMetrics = gradleTypes.Metrics{Profiles: profiles}
	// Create Metadata from GradleMetrics
	gradleMetadata := createMetadataFromGradleMetrics(savingsRequest.GradleMetrics)
	metadataBytes, err := json.Marshal(gradleMetadata)
	if err != nil {
		return cacheState, 0, savingsRequest, err
	}
	savingsRequest.Metadata = string(metadataBytes)
	return cacheState, buildTime, savingsRequest, nil
}
