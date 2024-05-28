package dlc

import (
	"encoding/json"
	"os"

	"github.com/harness/ti-client/types"
	"github.com/sirupsen/logrus"
)

type (
	// CacheMetrics represents the structure of the metrics data.
	CacheMetrics struct {
		TotalLayers int                 `json:"total_layers"`
		Done        int                 `json:"done"`
		Cached      int                 `json:"cached"`
		Errored     int                 `json:"errored"`
		Cancelled   int                 `json:"cancelled"`
		Layers      map[int]LayerStatus `json:"layers"`
	}

	// LayerStatus details the status of each layer.
	LayerStatus struct {
		Status string
		Time   float64 // Time in seconds; only set for DONE layers
	}
)

// GetFeatureState evaluates the execution state of a feature based on cache metrics.
func GetFeatureState(cacheMetricsFile string, log *logrus.Logger) (types.IntelligenceExecutionState, error) {
	// Initialize the state as DISABLED by default.
	state := types.DISABLED

	// Check if the file exists.
	if _, err := os.Stat(cacheMetricsFile); os.IsNotExist(err) {
		log.WithField("file", cacheMetricsFile).Error("Cache metrics file does not exist")
		return state, err
	}

	// Read the JSON file containing the cache metrics.
	data, err := os.ReadFile(cacheMetricsFile)
	if err != nil {
		log.WithField("file", cacheMetricsFile).Error("Failed to read cache metrics file")
		return state, err
	}

	// Deserialize the JSON data into the CacheMetrics struct.
	var metrics CacheMetrics
	if err := json.Unmarshal(data, &metrics); err != nil {
		log.WithField("file", cacheMetricsFile).Error("Failed to unmarshal cache metrics data")
		return state, err
	}

	// Determine the feature state based on the metrics.
	if metrics.TotalLayers > 0 {
		if metrics.Cached > 0 {
			state = types.OPTIMIZED // It's an optimized run if there are cached layers.
		} else {
			state = types.FULL_RUN // It's a full run if total layers are non-zero and no cached layers.
		}
	}

	return state, nil
}
