// Copyright 2022 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package logstream

// OpStats is the per-operation Log Service client tally for one stream.
type OpStats struct {
	Count      int64 `json:"count"`
	ErrorCount int64 `json:"error_count"`
	LatencyMs  int64 `json:"latency_ms"`
	Bytes      int64 `json:"bytes"`
}

// Stats aggregates open/write/close/upload RPCs for one log stream.
type Stats struct {
	Open   OpStats `json:"open"`
	Write  OpStats `json:"write"`
	Close  OpStats `json:"close"`
	Upload OpStats `json:"upload"`
}

// Empty reports whether any Log Service RPC was recorded.
func (s Stats) Empty() bool {
	return s.Open.Count == 0 && s.Write.Count == 0 && s.Close.Count == 0 && s.Upload.Count == 0
}

// StatsProvider is implemented by writers that tally Log Service RPCs.
type StatsProvider interface {
	LogServiceStats() Stats
}

// CopyStats returns a copy of Log Service stats when w implements StatsProvider.
func CopyStats(w Writer) *Stats {
	if p, ok := w.(StatsProvider); ok {
		s := p.LogServiceStats()
		if s.Empty() {
			return nil
		}
		return &s
	}
	return nil
}
