package pipeline

import (
	"fmt"
	"os"
	"sync"
	"testing"

	"github.com/harness/lite-engine/api"
	"github.com/harness/lite-engine/engine/spec"
	tiCfg "github.com/harness/lite-engine/ti/config"
	"github.com/stretchr/testify/assert"
)

func TestGetSharedVolPath(t *testing.T) {
	// Save and restore original env
	origWorkdir := os.Getenv("HARNESS_WORKDIR")
	defer os.Setenv("HARNESS_WORKDIR", origWorkdir)

	t.Run("no workdir returns default", func(t *testing.T) {
		os.Setenv("HARNESS_WORKDIR", "")
		assert.Equal(t, defaultSharedVolPath, GetSharedVolPath())
	})

	t.Run("workdir set returns workdir/engine", func(t *testing.T) {
		os.Setenv("HARNESS_WORKDIR", "/my/workdir")
		expected := "/my/workdir/tmp/engine"
		assert.Equal(t, expected, GetSharedVolPath())
	})

	t.Run("windows style workdir", func(t *testing.T) {
		os.Setenv("HARNESS_WORKDIR", "D:\\runner-workspace")
		expected := "D:\\runner-workspace/tmp/engine"
		assert.Equal(t, expected, GetSharedVolPath())
	})
}

// newTestState builds a fresh State without going through the package-level
// singleton, so tests don't have to share/reset GetState().
func newTestState() *State {
	return &State{
		osStatsEntries: make(map[string]*OSStatsEntry),
	}
}

// TestState_ConcurrentOSStatsEntries hammers the osStatsEntries map from many
// goroutines via Set/Get/Delete/GetAllOSStatsKeys. The map is the most
// concurrency-exposed piece of State — multiple stages register entries in
// parallel and handlers read them.
func TestState_ConcurrentOSStatsEntries(t *testing.T) {
	s := newTestState()

	const goroutines = 32
	const ops = 100
	var wg sync.WaitGroup

	// Writers
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < ops; j++ {
				key := fmt.Sprintf("k-%d-%d", i, j)
				s.SetOSStatsEntry(key, &OSStatsEntry{})
			}
		}(i)
	}
	// Readers
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < ops; j++ {
				_ = s.GetOSStatsEntry(fmt.Sprintf("k-%d-%d", i, j))
				_ = s.GetAllOSStatsKeys()
			}
		}(i)
	}
	// Deleters
	for i := 0; i < goroutines/2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < ops; j++ {
				s.DeleteOSStatsEntry(fmt.Sprintf("k-%d-%d", i, j))
			}
		}(i)
	}
	wg.Wait()
}

// TestState_ConcurrentSetGet exercises the simple field accessors under
// contention — Set/GetSecrets/GetLogConfig/GetTIConfig and the lite-engine
// log writer setters.
func TestState_ConcurrentSetGet(t *testing.T) {
	s := newTestState()

	const ops = 500
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < ops; i++ {
			s.Set(
				[]string{fmt.Sprintf("secret-%d", i)},
				api.LogConfig{URL: fmt.Sprintf("u-%d", i)},
				tiCfg.Cfg{},
				spec.MtlsConfig{},
				nil,
			)
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < ops; i++ {
			s.SetLELogWriter(nil, fmt.Sprintf("k-%d", i))
		}
	}()

	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < ops; j++ {
				_ = s.GetSecrets()
				_ = s.GetLogConfig()
				_ = s.GetTIConfig()
				_ = s.GetLELogKey()
				_ = s.GetLELogWriter()
				_ = s.GetStatsCollector()
			}
		}()
	}
	wg.Wait()
}

// TestState_ConcurrentSecretsIterate verifies that GetSecrets() callers can
// iterate the returned slice while Set() replaces s.secrets concurrently.
// This is safe in the current code because Set() assigns a brand-new slice
// (new backing array) — the old reader keeps iterating the prior backing
// array, untouched. Note: this would NOT be safe if any code path appended
// to s.secrets in place; it's only the wholesale-replace pattern that's OK.
func TestState_ConcurrentSecretsIterate(t *testing.T) {
	s := newTestState()
	s.Set([]string{"a", "b", "c"}, api.LogConfig{}, tiCfg.Cfg{}, spec.MtlsConfig{}, nil)

	const ops = 1000
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < ops; i++ {
			s.Set(
				[]string{fmt.Sprintf("s-%d-1", i), fmt.Sprintf("s-%d-2", i)},
				api.LogConfig{},
				tiCfg.Cfg{},
				spec.MtlsConfig{},
				nil,
			)
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		for j := 0; j < ops; j++ {
			secrets := s.GetSecrets()
			for _, sec := range secrets {
				_ = sec
			}
		}
	}()

	wg.Wait()
}

// TestState_ConcurrentLogStreamClient triggers the lazy-init branch in
// GetLogStreamClient() under contention. The mutex covers the construction,
// so we expect this to be clean — but the test pins down the assumption.
func TestState_ConcurrentLogStreamClient(t *testing.T) {
	s := newTestState()

	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				_ = s.GetLogStreamClient()
			}
		}()
	}
	wg.Wait()
}
