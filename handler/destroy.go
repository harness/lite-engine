// Copyright 2022 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package handler

import (
	"context"
	"encoding/json"
	"errors"
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

		stageLifecycleMu.Lock()
		defer stageLifecycleMu.Unlock()

		ctx := r.Context()
		pcUsed := pc.WasUsed()
		var destroyErr error
		pcStateUnavailable := pcUsed && !engine.PrivateConnectivityConfigured()
		if pcStateUnavailable {
			// PipelineConfig is intentionally process-local. If LE restarted after a PC setup, it no
			// longer has the Docker/network identifiers required to prove resource cleanup. Tailscale
			// logout is still attempted below, but the durable fence is retained and DRA must discard
			// the VM.
			destroyErr = fmt.Errorf(
				"private connectivity cleanup state is unavailable after lite-engine restart; discard this VM")
		} else {
			destroyErr = engine.Destroy(ctx)
		}

		// Keep connectivity until resources stop, then prove logout before this VM can be reused.
		// pcUsed also covers a restarted LE whose daemon is already logged out but whose reuse fence
		// must intentionally remain because resource cleanup can no longer be proven.
		if pcUsed || pc.NeedsNetworkCleanup() {
			log.WithField("pc_state_available", !pcStateUnavailable).
				Infoln("api: starting private connectivity logout during destroy")
			// DRA destroys this VM after /destroy even when cleanup reports an error.
			// Terminal cleanup may therefore use the documented Windows ephemeral-node
			// fallback; suspend continues to require strict immediate logout.
			if logoutErr := pc.LogoutForDisposal(ctx); logoutErr != nil {
				log.WithField("time", time.Now().Format(time.RFC3339)).
					WithError(logoutErr).
					Errorln("api: private connectivity logout failed")
				destroyErr = errors.Join(destroyErr, fmt.Errorf("pc logout failed: %w", logoutErr))
			}
		}
		if pcUsed && destroyErr == nil {
			destroyErr = pc.MarkCleanupComplete()
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

		stats := &spec.OSStats{}

		collector := state.GetStatsCollector()
		if collector != nil {
			collector.Stop()
			collector.Aggregate()
			stats = collector.Stats()
		}

		// Stop OS stats live streaming and close the writer (which flushes and uploads).
		// Close all OS stats streams so the P90 summary is always written before upload,
		// even if destroy is called without MemoryMetricsLogKey or stream was closed elsewhere.
		for _, key := range state.GetAllOSStatsKeys() {
			if err := closeOSStatsStream(state, key); err != nil {
				logger.FromRequest(r).
					WithField("time", time.Now().Format(time.RFC3339)).
					WithField("memory_metrics_log_key", key).
					WithError(err).
					Warnln("api: failed to close os stats stream")
			}
		}

		if destroyErr != nil {
			WriteError(w, fmt.Errorf("destroy error: %w", destroyErr))
			return
		}

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
func closeOSStatsStream(state *pipeline.State, key string) error {
	entry := state.GetOSStatsEntry(key)
	if entry == nil {
		return nil
	}

	// 1. Stop the stats collection goroutine (no more per-second lines)
	if entry.Cancel != nil {
		entry.Cancel()
	}

	logger.L.
		WithField("os_stats_key", key).
		Infoln("api: writing P90 summary to memory_metrics stream")

	// 2. Write the P90 summary to the stream before closing, so it is included in memory_metrics
	if entry.Writer != nil && entry.GetSummaryData != nil {
		cpuSamples, lastPayload, diskTotals := entry.GetSummaryData()
		osstats.WriteP90SummaryToStream(entry.Writer, cpuSamples, lastPayload, diskTotals, logger.L)
		// Flush so the summary is sent to the stream before Close() runs (upload + stream close)
		if lw, ok := entry.Writer.(*livelog.Writer); ok {
			_ = lw.Flush()
		}
	}

	// 3. Only then close the writer (flush and upload)
	if entry.Writer != nil {
		if err := entry.Writer.Close(); err != nil {
			return fmt.Errorf("failed to close os stats log writer: %w", err)
		}
	}

	// Remove the entry from state
	state.DeleteOSStatsEntry(key)
	return nil
}
