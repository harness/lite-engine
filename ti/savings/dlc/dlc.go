package dlc

import (
	"encoding/json"
	"os"

	"github.com/harness/ti-client/types"
	dlcTypes "github.com/harness/ti-client/types/cache/dlc"
	"github.com/sirupsen/logrus"
)

// GetFeatureState evaluates the execution state of a feature based on cache metrics.
func ParseDlcSavings(cacheMetricsFile string, log *logrus.Logger) (types.IntelligenceExecutionState, types.SavingsRequest, error) {
	savingsRequest := types.SavingsRequest{}
	// Initialize the state as DISABLED by default.
	state := types.DISABLED

	// Check if the file exists.
	if _, err := os.Stat(cacheMetricsFile); os.IsNotExist(err) {
		return state, savingsRequest, err
	}

	// Read the JSON file containing the cache metrics.
	data, err := os.ReadFile(cacheMetricsFile)
	if err != nil {
		return state, savingsRequest, err
	}

	// Deserialize the JSON data into the CacheMetrics struct.
	var metrics dlcTypes.Metrics
	if err := json.Unmarshal(data, &metrics); err != nil {
		return state, savingsRequest, err
	}

	var metadata dlcTypes.Metadata
	metadata.TotalLayers = metrics.TotalLayers
	metadata.Cached = metrics.Cached

	metadataBytes, err := json.Marshal(metadata)
	if err != nil {
		return state, savingsRequest, err
	}

	savingsRequest.Metadata = string(metadataBytes)

	savingsRequest.DlcMetrics = metrics

	// Determine the feature state based on the metrics.
	if metrics.TotalLayers > 0 {
		if metrics.Cached > 0 {
			state = types.OPTIMIZED // It's an optimized run if there are cached layers.
		} else {
			state = types.FULL_RUN // It's a full run if total layers are non-zero and no cached layers.
		}
	}

	return state, savingsRequest, nil
}
