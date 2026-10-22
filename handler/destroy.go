// Copyright 2022 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/harness/lite-engine/api"
	"github.com/harness/lite-engine/engine"
	"github.com/harness/lite-engine/engine/spec"
	"github.com/harness/lite-engine/livelog"
	"github.com/harness/lite-engine/logger"
	"github.com/harness/lite-engine/osstats"
	"github.com/harness/lite-engine/pc"
	"github.com/harness/lite-engine/pipeline"
	pruntime "github.com/harness/lite-engine/pipeline/runtime"
)

// HandleDestroy returns an http.HandlerFunc that destroy the stage resources
func HandleDestroy(engine *engine.Engine) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		st := time.Now()
		state := pipeline.GetState()
		log := logger.FromRequest(r)

		log.Infoln("api: destroy request received")

		var d api.DestroyRequest
		err := json.NewDecoder(r.Body).Decode(&d)
		if err != nil {
			WriteBadRequest(w, err)
			return
		}

		ctx := r.Context()
		pcEnabled := engine.PrivateConnectivityEnabled()
		destroyErr := engine.Destroy(ctx)

		if pcEnabled {
			// Destroy is terminal: DRA discards the VM even if this bounded best-effort
			// logout fails. Suspend remains strict because that VM can be reused.
			if logoutErr := pc.Logout(ctx); logoutErr != nil {
				log.WithField("time", time.Now().Format(time.RFC3339)).
					WithError(logoutErr).
					Errorln("api: private connectivity logout failed")
			} else {
				engine.ClearPrivateConnectivity()
			}
		}

		// Workload Identity: clear any handles still resident now that the stage is torn down and nothing
		// can mint. This is the backstop for detached steps, whose handle is not evicted on step completion.
		pruntime.ClearWorkloadIdentities()

		// Close lite-engine log stream to flush logs (always attempt so logs are uploaded)
		log.Infoln("api: closing lite-engine log stream")
		if closeErr := closeLELogStream(ctx, state); closeErr != nil {
			log.WithField("time", time.Now().Format(time.RFC3339)).
				WithError(closeErr).
				Warnln("api: failed to close lite-engine log stream")
		}

		if d.StageRuntimeID != "" {
			pipeline.GetEnvState().Delete(d.StageRuntimeID)
		}

		collector := state.GetStatsCollector()
		if collector != nil {
			collector.Stop()
		}

		stats := &spec.OSStats{}
		streamerPopulated := false

		// Stop OS stats live streaming and close the writer (which flushes and uploads).
		for _, key := range state.GetAllOSStatsKeys() {
			summary, err := closeOSStatsStream(state, key)
			if err != nil {
				logger.FromRequest(r).
					WithField("time", time.Now().Format(time.RFC3339)).
					WithField("memory_metrics_log_key", key).
					WithError(err).
					Warnln("api: failed to close os stats stream")
			}
			if summary != nil {
				streamerPopulated = true

				// CPU fields
				stats.CPUCores = summary.LastPayload.TotalCPU
				cpuPeak, cpuAvg := osstats.PeakAndAvg(summary.CPUSamples)
				stats.MaxCPUUsagePct = cpuPeak
				stats.AvgCPUUsagePct = cpuAvg
				stats.P50CPUUsagePct = osstats.PercentileNearestRank(summary.CPUSamples, 0.50) //nolint:mnd
				stats.P90CPUUsagePct = osstats.PercentileNearestRank(summary.CPUSamples, 0.90) //nolint:mnd
				stats.P95CPUUsagePct = osstats.PercentileNearestRank(summary.CPUSamples, 0.95) //nolint:mnd
				stats.P99CPUUsagePct = osstats.PercentileNearestRank(summary.CPUSamples, 0.99) //nolint:mnd

				// Memory fields (streamer stores memory in GB, convert to MB)
				gbToMB := 1024.0 //nolint:mnd
				stats.TotalMemMB = summary.LastPayload.TotalMemory * gbToMB
				memPeak, memAvg := osstats.PeakAndAvg(summary.MemSamples)
				stats.MaxMemUsagePct = memPeak
				stats.AvgMemUsagePct = memAvg
				stats.P50MemUsagePct = osstats.PercentileNearestRank(summary.MemSamples, 0.50) //nolint:mnd
				stats.P90MemUsagePct = osstats.PercentileNearestRank(summary.MemSamples, 0.90) //nolint:mnd
				stats.P95MemUsagePct = osstats.PercentileNearestRank(summary.MemSamples, 0.95) //nolint:mnd
				stats.P99MemUsagePct = osstats.PercentileNearestRank(summary.MemSamples, 0.99) //nolint:mnd

				// Disk I/O (already from streamer)
				stats.DiskTotalReadMB = float64(summary.DiskTotals.ReadBytes) / (1024 * 1024)   //nolint:mnd
				stats.DiskTotalWriteMB = float64(summary.DiskTotals.WriteBytes) / (1024 * 1024) //nolint:mnd
			}
		}

		// Fall back to collector when no streamer summary is available
		// (e.g. MemoryMetricsLogKey was not set, so the streamer was never started).
		if !streamerPopulated && collector != nil {
			collector.Aggregate()
			stats = collector.Stats()
		}

		if destroyErr != nil {
			WriteError(w, fmt.Errorf("destroy error: %w", destroyErr))
			return
		}

		logger.FromRequest(r).
			WithField("cpu_cores", stats.CPUCores).
			WithField("total_mem_mb", stats.TotalMemMB).
			WithField("avg_cpu_pct", stats.AvgCPUUsagePct).
			WithField("max_cpu_pct", stats.MaxCPUUsagePct).
			WithField("avg_mem_pct", stats.AvgMemUsagePct).
			WithField("max_mem_pct", stats.MaxMemUsagePct).
			WithField("p50_cpu_pct", stats.P50CPUUsagePct).
			WithField("p90_cpu_pct", stats.P90CPUUsagePct).
			WithField("p95_cpu_pct", stats.P95CPUUsagePct).
			WithField("p99_cpu_pct", stats.P99CPUUsagePct).
			WithField("p50_mem_pct", stats.P50MemUsagePct).
			WithField("p90_mem_pct", stats.P90MemUsagePct).
			WithField("p95_mem_pct", stats.P95MemUsagePct).
			WithField("p99_mem_pct", stats.P99MemUsagePct).
			WithField("disk_read_mb", stats.DiskTotalReadMB).
			WithField("disk_write_mb", stats.DiskTotalWriteMB).
			Infoln("destroy: returning resource utilization stats")

		WriteJSON(w, api.DestroyResponse{OSStats: stats}, http.StatusOK)

		logger.FromRequest(r).
			WithField("latency", time.Since(st)).
			WithField("time", time.Now().Format(time.RFC3339)).
			Infoln("api: successfully destroyed the stage resources")
	}
}

// closeLELogStream closes the lite-engine log stream writer if it exists.
func closeLELogStream(_ context.Context, state *pipeline.State) error {
	writer := state.GetLELogWriter()
	if writer == nil {
		return nil
	}

	logKey := state.GetLELogKey()
	logger.L.
		WithField("le_log_key", logKey).
		Infoln("api: closing lite-engine log stream")

	// Close the writer to flush and upload remaining logs
	if err := writer.Close(); err != nil {
		return fmt.Errorf("failed to close lite-engine log writer: %w", err)
	}

	// Clear the writer from state
	state.SetLELogWriter(nil, "")
	return nil
}

// closeOSStatsStream stops the OS stats collection, writes the P90 summary to the stream,
// then closes the writer so the memory_metrics file always ends with the summary line.
// Returns the full StreamerSummary (CPU samples, memory samples, last payload, disk totals)
// captured after the sampling goroutine has fully exited (Cancel calls wg.Wait), so all
// reads are free from data races.
func closeOSStatsStream(state *pipeline.State, key string) (*osstats.StreamerSummary, error) {
	entry := state.GetOSStatsEntry(key)
	if entry == nil {
		return nil, nil
	}

	// 1. Stop the stats collection goroutine (no more per-second lines).
	//    Cancel() closes the done channel and calls wg.Wait(), so after this
	//    returns the goroutine is fully stopped and all state is safe to read.
	if entry.Cancel != nil {
		entry.Cancel()
	}

	logger.L.
		WithField("os_stats_key", key).
		Infoln("api: writing P90 summary to memory_metrics stream")

	// 2. Write the P90 summary to the stream before closing, so it is included in memory_metrics.
	//    GetSummaryData is now safe because the goroutine has exited above.
	var summary *osstats.StreamerSummary
	if entry.Writer != nil && entry.GetSummaryData != nil {
		s := entry.GetSummaryData()
		summary = &s
		osstats.WriteP90SummaryToStream(entry.Writer, s.CPUSamples, s.LastPayload, s.DiskTotals, logger.L)
		// Flush so the summary is sent to the stream before Close() runs (upload + stream close)
		if lw, ok := entry.Writer.(*livelog.Writer); ok {
			_ = lw.Flush()
		}
	}

	// 3. Only then close the writer (flush and upload)
	if entry.Writer != nil {
		if err := entry.Writer.Close(); err != nil {
			return summary, fmt.Errorf("failed to close os stats log writer: %w", err)
		}
	}

	// Remove the entry from state
	state.DeleteOSStatsEntry(key)
	return summary, nil
}
