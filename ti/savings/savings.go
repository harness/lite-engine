package savings

import (
	"context"
	"strconv"
	"strings"
	"time"

	tiCfg "github.com/harness/lite-engine/ti/config"
	"github.com/harness/lite-engine/ti/savings/cache"
	"github.com/harness/lite-engine/ti/savings/dlc"
	"github.com/harness/ti-client/types"
	"github.com/sirupsen/logrus"
)

func ParseAndUploadSavings(ctx context.Context, workspace string, log *logrus.Logger, stepID string, cmdTimeTaken int64,
	tiConfig *tiCfg.Cfg, envs map[string]string) types.IntelligenceExecutionState {
	states := make([]types.IntelligenceExecutionState, 0)
	// Cache Savings
	start := time.Now()
	cacheState, timeTaken, savingsRequest, err := cache.ParseCacheSavings(workspace, log)
	if err == nil {
		states = append(states, cacheState)
		log.Infof("Computed build cache execution details with state %s and time %sms in %0.2f seconds",
			cacheState, strconv.Itoa(timeTaken), time.Since(start).Seconds())

		tiStart := time.Now()
		tiErr := tiConfig.GetClient().WriteSavings(ctx, stepID, types.BUILD_CACHE, cacheState, int64(timeTaken), savingsRequest)
		if tiErr == nil {
			log.Infof("Successfully uploaded savings for feature %s in %0.2f seconds",
				types.BUILD_CACHE, time.Since(tiStart).Seconds())
		}
	}

	// TI Savings
	if tiState, err := tiConfig.GetFeatureState(stepID, types.TI); err == nil {
		states = append(states, tiState)
		log.Infof("Computed test intelligence execution details with state %s and time %dms",
			tiState, cmdTimeTaken)

		tiStart := time.Now()
		tiErr := tiConfig.GetClient().WriteSavings(ctx, stepID, types.TI, tiState, cmdTimeTaken, types.SavingsRequest{})
		if tiErr == nil {
			log.Infof("Successfully uploaded savings for feature %s in %0.2f seconds",
				types.TI, time.Since(tiStart).Seconds())
		}
	}

	// DLC Savings
	if cacheMetricsFile, found := envs["PLUGIN_CACHE_METRICS_FILE"]; found {
		if opts, ok := envs["PLUGIN_BUILDER_DRIVER_OPTS"]; ok && strings.Contains(opts, "harness/buildkit") {
			dlcState, savingsRequest, err := dlc.ParseDlcSavings(cacheMetricsFile, log)
			if err == nil {
				states = append(states, dlcState)
				log.Infof("Computed docker layer caching execution details with state %s and time %dms", dlcState, cmdTimeTaken)
				tiStart := time.Now()
				tiErr := tiConfig.GetClient().WriteSavings(ctx, stepID, types.DLC, dlcState, cmdTimeTaken, savingsRequest)
				if tiErr == nil {
					log.Infof("Successfully uploaded savings for feature %s in %0.2f seconds",
						types.DLC, time.Since(tiStart).Seconds())
				}
			}
		}
	}
	// Cache Intel savings (Placeholder)
	return getStepState(states)
}

func getStepState(states []types.IntelligenceExecutionState) types.IntelligenceExecutionState {
	state := types.DISABLED
	for _, s := range states {
		switch s {
		case types.OPTIMIZED:
			return s
		case types.FULL_RUN:
			state = s
		case types.DISABLED:
			continue
		default:
			continue
		}
	}
	return state
}
