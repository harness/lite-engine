package savings

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/harness/lite-engine/common"
	tiCfg "github.com/harness/lite-engine/ti/config"
	"github.com/harness/lite-engine/ti/savings/cache"
	"github.com/harness/lite-engine/ti/savings/cache/gradle"
	"github.com/harness/lite-engine/ti/savings/dlc"
	"github.com/harness/ti-client/types"

	"github.com/sirupsen/logrus"
)

const restoreCacheHarnessStepID = "restore-cache-harness"

func ParseAndUploadSavings(ctx context.Context, workspace string, log *logrus.Logger, stepID string, stepSuccess bool, cmdTimeTaken int64,
	tiConfig *tiCfg.Cfg, envs map[string]string, telemetryData *types.TelemetryData, stepType string) types.IntelligenceExecutionState {
	states := make([]types.IntelligenceExecutionState, 0)

	// Cache Savings
	// Only parse build cache savings if its not a plugin step
	if stepType != common.StepTypePlugin {
		start := time.Now()
		cacheState, timeTaken, savingsRequest, err := cache.ParseCacheSavings(workspace, log, cmdTimeTaken, telemetryData)
		if err == nil {
			states = append(states, cacheState)
			log.Infof("Computed build cache execution details with state %s and time %sms in %0.2f seconds",
				cacheState, strconv.Itoa(timeTaken), time.Since(start).Seconds())

			tiStart := time.Now()
			tiErr := tiConfig.GetClient().WriteSavings(ctx, stepID, types.BUILD_CACHE, cacheState, int64(timeTaken), savingsRequest)
			if tiErr == nil {
				log.Infof("Successfully uploaded savings for feature %s in %0.2f seconds",
					types.BUILD_CACHE, time.Since(tiStart).Seconds())
			} else {
				log.Errorf("Failed to upload savings for feature %s: %v", types.BUILD_CACHE, tiErr)
				fmt.Println("Failed to upload savings for feature :", types.BUILD_CACHE, tiErr)
			}

			totaltasks, cachedtasks := gradle.GetMetadataFromGradleMetrics(&savingsRequest)
			telemetryData.BuildIntelligenceMetaData.BuildTasks = totaltasks
			telemetryData.BuildIntelligenceMetaData.TasksRestored = cachedtasks
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
		if _, ok := envs["PLUGIN_BUILDER_DRIVER_OPTS"]; ok {
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
				telemetryData.DlcMetadata.TotalLayers = savingsRequest.DlcMetrics.TotalLayers
				telemetryData.DlcMetadata.Cached = savingsRequest.DlcMetrics.Cached
			}
		}
	}

	// No Savings should happen beyond this except for Cache Intel
	// If the Step Fails then we should return the savings state only for restore-cache (CACHE_INTEL). This is to match the current behavior in K8
	if !stepSuccess {
		states = make([]types.IntelligenceExecutionState, 0)
	}

	// Cache Intel savings
	if stepID == restoreCacheHarnessStepID {
		cacheIntelState := types.FULL_RUN
		if stepSuccess {
			cacheIntelState = types.OPTIMIZED
		}
		states = append(states, cacheIntelState)
		if cacheIntelFile, found := envs["PLUGIN_CACHE_INTEL_METRICS_FILE"]; found {
			err := parseCacheInfo(cacheIntelFile, telemetryData)
			if err != nil {
				log.Errorf("skipping cache metrics parsing: %v", err)
			}
		}
	}

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

func parseCacheInfo(cacheIntelFile string, telemetryData *types.TelemetryData) error {
	// Check if the file exists.
	if _, err := os.Stat(cacheIntelFile); os.IsNotExist(err) {
		return err
	}

	// Read the JSON file containing the cache metrics.
	data, err := os.ReadFile(cacheIntelFile)
	if err != nil {
		return err
	}

	// Deserialize the JSON data into a slice of CacheMetadata.
	var cacheInfoList []types.CacheMetadata
	if err := json.Unmarshal(data, &cacheInfoList); err != nil {
		return err
	}

	// Calculate the total cache size (raw bytes).
	var totalCacheSize uint64
	for _, cacheInfo := range cacheInfoList {
		totalCacheSize += cacheInfo.CacheSizeBytes
	}

	// Set the total cache size in telemetry data (human-readable format).
	telemetryData.CacheIntelligenceMetaData.CacheSize = totalCacheSize
	return nil
}
