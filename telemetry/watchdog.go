// Copyright 2026 Harness Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package telemetry

import (
	"sync/atomic"

	"github.com/sirupsen/logrus"
)

const (
	// resourceOverLimitMessage is the stable body of the warning, so it can be
	// searched for in the log backend.
	resourceOverLimitMessage = "lite-engine resource usage above limit"

	fieldResource = "resource"
	fieldValue    = "value"
	fieldLimit    = "limit"

	resourceGoroutines = "goroutines"
	resourceOpenFDs    = "open_fds"

	// rearmPercent is how far below its limit a reading must fall before the
	// next crossing is reported again. It keeps a process hovering around the
	// limit from producing a warning per sample.
	rearmPercent = 80

	defaultGoroutineLimit = 1000
	defaultOpenFDLimit    = 1024
)

// limits are the readings above which lite-engine reports itself as unhealthy.
// A limit that is not positive disables that check.
type limits struct {
	goroutines int
	openFDs    int
}

// defaultLimits are deliberately aligned with histogram bucket bounds (see
// goroutineBuckets and fdBuckets), so that "share of VMs above the limit" can
// be read from the fleet histograms exactly.
var defaultLimits = limits{goroutines: defaultGoroutineLimit, openFDs: defaultOpenFDLimit}

// samplers is a Sampler that runs every member, even when one of them fails.
type samplers []Sampler

var _ Sampler = samplers(nil)

// Sample implements Sampler.
func (s samplers) Sample() {
	for _, member := range s {
		contain(member.Sample)
	}
}

// excursion tracks whether one reading is currently over its limit, so that a
// sustained excursion is reported once. It is safe for concurrent use.
type excursion struct {
	resource string
	limit    int
	active   atomic.Bool
}

// begins reports whether v starts a new excursion: it is at or over the limit
// and the previous one, if any, has ended. An excursion ends once a reading
// falls clearly below the limit.
func (e *excursion) begins(v int) bool {
	if e.limit <= 0 {
		return false
	}
	if v >= e.limit {
		return e.active.CompareAndSwap(false, true)
	}
	if v < e.limit*rearmPercent/100 {
		e.active.Store(false)
	}
	return false
}

// resourceWatch is a Sampler that logs a warning when goroutines or open file
// descriptors cross a limit.
//
// Metrics in fleet mode have no VM identity, so they show that some VMs are
// leaking but not which. Logs do carry vm_id, so this warning is what names
// the VM, at the cost of a single log line per excursion instead of a
// per-VM metric series.
type resourceWatch struct {
	stats      ProcessStats
	logger     *logrus.Logger
	goroutines excursion
	openFDs    excursion
}

var _ Sampler = (*resourceWatch)(nil)

func newResourceWatch(stats ProcessStats, logger *logrus.Logger, l limits) *resourceWatch {
	return &resourceWatch{
		stats:      stats,
		logger:     logger,
		goroutines: excursion{resource: resourceGoroutines, limit: l.goroutines},
		openFDs:    excursion{resource: resourceOpenFDs, limit: l.openFDs},
	}
}

// Sample implements Sampler.
func (w *resourceWatch) Sample() {
	w.check(&w.goroutines, w.stats.Goroutines())
	// An unreadable count is no information: it neither warns nor ends an
	// excursion.
	if n, ok := w.stats.OpenFDs(); ok {
		w.check(&w.openFDs, n)
	}
}

func (w *resourceWatch) check(e *excursion, value int) {
	if !e.begins(value) {
		return
	}
	w.logger.WithFields(logrus.Fields{
		fieldResource: e.resource,
		fieldValue:    value,
		fieldLimit:    e.limit,
	}).Warn(resourceOverLimitMessage)
}
