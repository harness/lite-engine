package cache

import (
	"errors"
	"strings"

	"github.com/harness/lite-engine/pipeline"
	"github.com/harness/lite-engine/ti/savings/cache/gradle"
	"github.com/harness/lite-engine/ti/savings/cache/maven"
	"github.com/harness/ti-client/types"
	gradleTypes "github.com/harness/ti-client/types/cache/gradle"
	mavenTypes "github.com/harness/ti-client/types/cache/maven"
	"github.com/sirupsen/logrus"
)

func joinErrors(errs ...error) error {
	var messages []string
	for _, err := range errs {
		if err != nil {
			messages = append(messages, err.Error())
		}
	}
	if len(messages) == 0 {
		return nil
	}
	return errors.New(strings.Join(messages, "; "))
}

func ParseCacheSavings(workspace string, log *logrus.Logger, cmdTimeTaken int64, telemetryData *types.TelemetryData) (types.IntelligenceExecutionState, int, types.SavingsRequest, error) {
	savingsRequest := types.SavingsRequest{}

	cacheState := types.DISABLED

	// TODO: This assumes that savings data is only present for Gradle. Refactor when more cache options are available
	gradleCacheState, profiles, buildTime, gradleErr := gradle.ParseSavings(workspace, log)
	savingsRequest.GradleMetrics = gradleTypes.Metrics{Profiles: profiles}

	// Check for build tool marker files before returning telemetry data
	checkBuildToolMarkers(pipeline.GetSharedVolPath(), telemetryData, log)

	if gradleCacheState == types.DISABLED && telemetryData.BuildIntelligenceMetaData.IsGradleBIUsed {
		log.Infof("Build Intelligence savings data is unavailable. To view savings data in the UI, please add the --profile flag to your Gradle command.")
	}

	mavenCacheState, reports, mavenErr := maven.ParseSavings(workspace, log)

	// Bazel exports no cache reports (unlike gradle/maven above), so the BI
	// marker file is the only available signal for it. Don't bail out via
	// the gradle/maven failure gate below when it's present, so a Bazel-only
	// build (which never produces gradle or maven report files) can still
	// surface savings.
	isBazelBIUsed := telemetryData.BuildIntelligenceMetaData.IsBazelBIUsed

	if gradleErr != nil && mavenErr != nil && !isBazelBIUsed {
		return types.FULL_RUN, 0, savingsRequest, joinErrors(gradleErr, mavenErr)
	}

	// Update cacheState based on gradle results if gradle parsing succeeded
	if gradleErr == nil {
		cacheState = gradleCacheState
	}

	if mavenCacheState == types.OPTIMIZED {
		cacheState = mavenCacheState
		buildTime = int(cmdTimeTaken)
	}

	// The marker fires on both cache hits (restore) and misses (save,
	// populating the cache after a full run), so it can't currently tell
	// the two apart. Reporting OPTIMIZED whenever it's set is intentionally
	// optimistic per product decision - a fully-missed build will overstate
	// its savings until harness-cache records hit/miss instead of just usage.
	if isBazelBIUsed {
		cacheState = types.OPTIMIZED
		buildTime = int(cmdTimeTaken)
	}

	savingsRequest.MavenMetrics = mavenTypes.MavenMetrics{Reports: reports}
	return cacheState, buildTime, savingsRequest, nil
}
