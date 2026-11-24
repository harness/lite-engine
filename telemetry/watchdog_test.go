package telemetry

import (
	"context"
	"sync"
	"testing"

	"github.com/sirupsen/logrus"
	logtest "github.com/sirupsen/logrus/hooks/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	otellog "go.opentelemetry.io/otel/log"
)

const (
	testGoroutineLimit = 1000
	testFDLimit        = 1024
)

type watchHarness struct {
	watch *resourceWatch
	stats *fakeProcessStats
	hook  *logtest.Hook
}

func newWatchHarness(l limits) *watchHarness {
	logger, hook := logtest.NewNullLogger()
	stats := &fakeProcessStats{goroutines: 40, fds: 20, fdsOK: true}
	return &watchHarness{watch: newResourceWatch(stats, logger, l), stats: stats, hook: hook}
}

func testLimits() limits { return limits{goroutines: testGoroutineLimit, openFDs: testFDLimit} }

func (h *watchHarness) setGoroutines(n int) {
	h.stats.set(func(f *fakeProcessStats) { f.goroutines = n })
}
func (h *watchHarness) setFDs(n int) { h.stats.set(func(f *fakeProcessStats) { f.fds = n }) }

func (h *watchHarness) warnings() []*logrus.Entry {
	var out []*logrus.Entry
	for _, e := range h.hook.AllEntries() {
		if e.Level == logrus.WarnLevel {
			out = append(out, e)
		}
	}
	return out
}

func TestResourceWatch_StaysSilentWhileUsageIsBelowTheLimit(t *testing.T) {
	h := newWatchHarness(testLimits())

	h.setGoroutines(testGoroutineLimit - 1)
	h.setFDs(testFDLimit - 1)
	h.watch.Sample()

	assert.Empty(t, h.hook.AllEntries())
}

func TestResourceWatch_WarnsOnceWithTheReadingWhenGoroutinesCrossTheLimit(t *testing.T) {
	h := newWatchHarness(testLimits())

	h.setGoroutines(testGoroutineLimit + 250)
	h.watch.Sample()

	warns := h.warnings()
	require.Len(t, warns, 1)
	assert.Equal(t, resourceOverLimitMessage, warns[0].Message)
	assert.Equal(t, "goroutines", warns[0].Data[fieldResource])
	assert.Equal(t, testGoroutineLimit+250, warns[0].Data[fieldValue])
	assert.Equal(t, testGoroutineLimit, warns[0].Data[fieldLimit])
}

func TestResourceWatch_ReachingTheLimitExactlyCounts(t *testing.T) {
	h := newWatchHarness(testLimits())

	h.setGoroutines(testGoroutineLimit)
	h.watch.Sample()

	assert.Len(t, h.warnings(), 1)
}

func TestResourceWatch_WarnsWhenOpenFDsCrossTheLimit(t *testing.T) {
	h := newWatchHarness(testLimits())

	h.setFDs(testFDLimit + 1)
	h.watch.Sample()

	warns := h.warnings()
	require.Len(t, warns, 1)
	assert.Equal(t, "open_fds", warns[0].Data[fieldResource])
	assert.Equal(t, testFDLimit+1, warns[0].Data[fieldValue])
	assert.Equal(t, testFDLimit, warns[0].Data[fieldLimit])
}

func TestResourceWatch_ResourcesAreTrackedIndependently(t *testing.T) {
	h := newWatchHarness(testLimits())

	h.setGoroutines(testGoroutineLimit + 1)
	h.watch.Sample()
	h.setFDs(testFDLimit + 1)
	h.watch.Sample()

	warns := h.warnings()
	require.Len(t, warns, 2, "an FD leak after a goroutine leak is news, not a repeat")
	assert.Equal(t, "goroutines", warns[0].Data[fieldResource])
	assert.Equal(t, "open_fds", warns[1].Data[fieldResource])
}

func TestResourceWatch_StaysQuietWhileUsageRemainsOverTheLimit(t *testing.T) {
	h := newWatchHarness(testLimits())

	for _, n := range []int{1100, 5000, 90000, 1100} {
		h.setGoroutines(n)
		h.watch.Sample()
	}

	assert.Len(t, h.warnings(), 1, "one warning per excursion, not one per sample")
}

func TestResourceWatch_WarnsAgainOnlyAfterUsageClearlyRecovers(t *testing.T) {
	h := newWatchHarness(testLimits())
	h.setGoroutines(testGoroutineLimit + 100)
	h.watch.Sample()

	// Hovering just under the limit is the same excursion: no flapping.
	h.setGoroutines(testGoroutineLimit - 10)
	h.watch.Sample()
	h.setGoroutines(testGoroutineLimit + 10)
	h.watch.Sample()
	require.Len(t, h.warnings(), 1)

	// A real recovery re-arms the watch, so a second leak is reported.
	h.setGoroutines(testGoroutineLimit / 2)
	h.watch.Sample()
	h.setGoroutines(testGoroutineLimit + 500)
	h.watch.Sample()

	assert.Len(t, h.warnings(), 2)
}

func TestResourceWatch_AnUnreadableFDCountIsNeitherAWarningNorAReset(t *testing.T) {
	h := newWatchHarness(testLimits())
	h.setFDs(testFDLimit + 1)
	h.watch.Sample()

	h.stats.set(func(f *fakeProcessStats) { f.fdsOK = false; f.fds = 0 })
	h.watch.Sample()
	h.stats.set(func(f *fakeProcessStats) { f.fdsOK = true; f.fds = testFDLimit + 1 })
	h.watch.Sample()

	assert.Len(t, h.warnings(), 1, "a gap in readings must not look like a recovery")
}

func TestResourceWatch_ANonPositiveLimitDisablesThatCheck(t *testing.T) {
	h := newWatchHarness(limits{goroutines: 0, openFDs: -1})

	h.setGoroutines(1_000_000)
	h.setFDs(1_000_000)
	h.watch.Sample()

	assert.Empty(t, h.hook.AllEntries())
}

func TestResourceWatch_ConcurrentSamplesReportAnExcursionExactlyOnce(t *testing.T) {
	h := newWatchHarness(testLimits())
	h.setGoroutines(testGoroutineLimit * 3)
	h.setFDs(testFDLimit * 3)

	var wg sync.WaitGroup
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			h.watch.Sample()
		}()
	}
	wg.Wait()

	assert.Len(t, h.warnings(), 2, "one per resource, however many samplers race")
}

func TestResourceWatch_ConcurrentSamplesWhileUsageOscillatesAreRaceFree(t *testing.T) {
	h := newWatchHarness(testLimits())

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				h.watch.Sample()
			}
		}()
		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				h.setGoroutines(map[bool]int{true: 5000, false: 10}[j%2 == 0])
			}
		}()
	}
	wg.Wait()
}

func TestResourceWatch_Limits_AreBucketBoundsOfTheFleetHistograms(t *testing.T) {
	// The share of samples above a limit can then be read straight off the
	// histogram, with no interpolation.
	assert.Contains(t, goroutineBuckets, float64(defaultLimits.goroutines))
	assert.Contains(t, fdBuckets, float64(defaultLimits.openFDs))
}

func TestHistogramBuckets_AreStrictlyAscending(t *testing.T) {
	for name, buckets := range map[string][]float64{"goroutines": goroutineBuckets, "fds": fdBuckets} {
		for i := 1; i < len(buckets); i++ {
			assert.Less(t, buckets[i-1], buckets[i], "%s buckets must ascend", name)
		}
	}
}

func TestHistogramBuckets_ResolveLeaksFinerThanPowersOfTwo(t *testing.T) {
	// 129 and 255 shared a bucket before; a leak is visible while it grows.
	assert.NotEqual(t, bucketIndex(goroutineBuckets, 150), bucketIndex(goroutineBuckets, 250))
	assert.NotEqual(t, bucketIndex(goroutineBuckets, 400), bucketIndex(goroutineBuckets, 600))
	assert.NotEqual(t, bucketIndex(fdBuckets, 200), bucketIndex(fdBuckets, 300))
	assert.NotEqual(t, bucketIndex(fdBuckets, 600), bucketIndex(fdBuckets, 900))
}

// bucketIndex is the explicit-bounds bucket (upper bound inclusive) v falls in.
func bucketIndex(bounds []float64, v float64) int {
	for i, b := range bounds {
		if v <= b {
			return i
		}
	}
	return len(bounds)
}

type orderedSampler struct {
	mu    sync.Mutex
	order *[]string
	name  string
	boom  bool
}

func (s *orderedSampler) Sample() {
	s.mu.Lock()
	*s.order = append(*s.order, s.name)
	s.mu.Unlock()
	if s.boom {
		panic("boom")
	}
}

func TestSamplers_RunEveryMemberInOrder(t *testing.T) {
	var order []string

	samplers{&orderedSampler{order: &order, name: "a"}, &orderedSampler{order: &order, name: "b"}}.Sample()

	assert.Equal(t, []string{"a", "b"}, order)
}

func TestSamplers_AFailingMemberDoesNotStarveTheOthers(t *testing.T) {
	var order []string

	require.NotPanics(t, func() {
		samplers{&orderedSampler{order: &order, name: "a", boom: true}, &orderedSampler{order: &order, name: "b"}}.Sample()
	})

	assert.Equal(t, []string{"a", "b"}, order)
}

// ---- wired through the controller ------------------------------------------

func TestController_ShipsAResourceWarningAsALogRecordWithVMIdentity(t *testing.T) {
	h := newControllerHarness(t, false)
	h.stats.set(func(f *fakeProcessStats) { f.goroutines = defaultLimits.goroutines + 50 })

	require.NoError(t, h.ctrl.Start(enabledConfig()))
	require.NoError(t, h.ctrl.Flush(context.Background()))

	var found *exportedLog
	for _, l := range h.logs.snapshot() {
		if l.body == resourceOverLimitMessage {
			found = &l
		}
	}
	require.NotNil(t, found, "the over-limit warning must reach the log pipeline")
	assert.Equal(t, otellog.SeverityWarn, found.severity)
	assert.Equal(t, "vm-9", found.attrs["vm_id"], "this is the log that names the VM")
	assert.Equal(t, "goroutines", found.attrs[fieldResource])
	assert.Equal(t, "1050", found.attrs[fieldValue])
}

func TestController_ShipsAResourceWarningInVMIDModeToo(t *testing.T) {
	h := newControllerHarness(t, true)
	h.stats.set(func(f *fakeProcessStats) { f.fds = defaultLimits.openFDs + 1 })
	cfg := enabledConfig()
	cfg.MetricsVMID = true

	require.NoError(t, h.ctrl.Start(cfg))
	require.NoError(t, h.ctrl.Flush(context.Background()))

	var seen bool
	for _, l := range h.logs.snapshot() {
		seen = seen || (l.body == resourceOverLimitMessage && l.attrs[fieldResource] == "open_fds")
	}
	assert.True(t, seen, "gauge mode does not sample into histograms but must still watch")
}

func TestController_HealthyProcessEmitsNoResourceWarning(t *testing.T) {
	h := newControllerHarness(t, false)

	require.NoError(t, h.ctrl.Start(enabledConfig()))
	require.NoError(t, h.ctrl.Flush(context.Background()))

	for _, l := range h.logs.snapshot() {
		assert.NotEqual(t, resourceOverLimitMessage, l.body)
	}
}
