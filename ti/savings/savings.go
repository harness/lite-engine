package savings

import (
	"context"
	"strconv"
	"time"

	tiCfg "github.com/harness/lite-engine/ti/config"
	"github.com/harness/lite-engine/ti/savings/cache"
	"github.com/harness/ti-client/types"
	"github.com/sirupsen/logrus"
)

func ParseAndUploadSavings(ctx context.Context, workspace string, log *logrus.Logger, stepID string,
	cmdTimeTaken int64, tiConfig *tiCfg.Cfg) {
	// Cache Savings
	start := time.Now()
	cacheState, timeTaken, err := cache.ParseCacheSavings(workspace, log)
	if err == nil {
		log.Infof("Computed build cache execution details with state %s and time %sms in %0.2f seconds",
			cacheState, strconv.Itoa(timeTaken), time.Since(start).Seconds())

		tiStart := time.Now()
		tiErr := tiConfig.GetClient().WriteSavings(ctx, stepID, types.BUILD_CACHE, cacheState, int64(timeTaken))
		if tiErr == nil {
			log.Infof("Successfully uploaded savings for feature %s in %0.2f seconds",
				types.BUILD_CACHE, time.Since(tiStart).Seconds())
		}
	}

	// TI Savings
	if tiState, err := tiConfig.GetSavingsState(stepID, types.TI); err == nil {
		log.Infof("Computed test intelligence execution details with state %s and time %dms",
			tiState, cmdTimeTaken)

		tiStart := time.Now()
		tiErr := tiConfig.GetClient().WriteSavings(ctx, stepID, types.TI, tiState, cmdTimeTaken)
		if tiErr == nil {
			log.Infof("Successfully uploaded savings for feature %s in %0.2f seconds",
				types.TI, time.Since(tiStart).Seconds())
		}
	}

	// DLC Savings (Placeholder)
	// Cache Intel savings (Placeholder)
}
