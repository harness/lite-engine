package savings

import (
	"context"

	tiCfg "github.com/harness/lite-engine/ti/config"
	"github.com/harness/lite-engine/ti/savings/cache"
	"github.com/harness/ti-client/types"
	"github.com/sirupsen/logrus"
)

func ParseAndUploadSavings(ctx context.Context, workspace string, log *logrus.Logger, stepID string, tiConfig *tiCfg.Cfg) {
	// Cache Savings
	state, time, err := cache.ParseCacheSavings(workspace, log)
	if err == nil {
		_ = tiConfig.GetClient().WriteSavings(ctx, stepID, types.BUILD_CACHE, state, int64(time))
	}
	// DLC Savings (Placeholder)
	// Cache Intel savings (Placeholder)
}
