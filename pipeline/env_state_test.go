// Copyright 2022 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package pipeline

import (
	"fmt"
	"sync"
	"testing"
)

func TestEnvState_AddGetDelete(t *testing.T) {
	s := &EnvState{env: make(map[string]map[string]string)}

	s.Add("stage-1", map[string]string{"K": "V"})
	got := s.Get("stage-1")
	if got["K"] != "V" {
		t.Fatalf("got %q, want V", got["K"])
	}
	s.Delete("stage-1")
	if got := s.Get("stage-1"); got != nil {
		t.Fatalf("got %v, want nil", got)
	}
}

func TestEnvState_AddMergesKeys(t *testing.T) {
	s := &EnvState{env: make(map[string]map[string]string)}
	s.Add("stage-1", map[string]string{"A": "1"})
	s.Add("stage-1", map[string]string{"B": "2"})
	got := s.Get("stage-1")
	if got["A"] != "1" || got["B"] != "2" {
		t.Fatalf("merge failed: %v", got)
	}
}

func TestGetEnvState_Singleton(t *testing.T) {
	a := GetEnvState()
	b := GetEnvState()
	if a != b {
		t.Fatalf("GetEnvState returned different instances")
	}
}

// TestEnvState_ConcurrentDistinctStages — many goroutines, each writing/
// reading its own stage key. This exercises the outer map under concurrent
// Add/Get/Delete with no key overlap.
func TestEnvState_ConcurrentDistinctStages(t *testing.T) {
	s := &EnvState{env: make(map[string]map[string]string)}

	const goroutines = 32
	const ops = 100
	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			key := fmt.Sprintf("stage-%d", i)
			for j := 0; j < ops; j++ {
				s.Add(key, map[string]string{fmt.Sprintf("k%d", j): fmt.Sprintf("v%d", j)})
				_ = s.Get(key)
			}
			s.Delete(key)
		}(i)
	}
	wg.Wait()
}

// TestEnvState_ConcurrentSameStage stresses the inner map: many goroutines
// hitting Add/Get/Delete on the SAME stage key. This is the case where the
// race is most likely to surface — Get() currently returns the inner map
// reference under the lock, but Add() then mutates that same inner map.
// A caller iterating over Get()'s return value while another goroutine
// calls Add() will race.
//
// We don't iterate the returned map here (that would be the caller's bug,
// not the package's), but we do exercise the hot path under heavy contention
// to catch any internal accounting races.
func TestEnvState_ConcurrentSameStage(t *testing.T) {
	s := &EnvState{env: make(map[string]map[string]string)}

	const key = "shared"
	const goroutines = 16
	const ops = 200
	var wg sync.WaitGroup

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < ops; j++ {
				s.Add(key, map[string]string{fmt.Sprintf("k%d-%d", i, j): "v"})
			}
		}(i)
	}
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < ops; j++ {
				_ = s.Get(key)
			}
		}()
	}
	wg.Wait()
}

// TestEnvState_ConcurrentGetIterate demonstrates the API-level race in Get():
// it returns the live inner map without copying. A caller iterating over that
// map while another goroutine calls Add() on the same stage will race.
//
// This test SHOULD fail under -race in the current code — it's documenting a
// real bug in the public API contract, not a problem in the test setup.
func TestEnvState_ConcurrentGetIterate(t *testing.T) {
	s := &EnvState{env: make(map[string]map[string]string)}
	const key = "iter"
	s.Add(key, map[string]string{"seed": "0"})

	const ops = 1000
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < ops; i++ {
			s.Add(key, map[string]string{fmt.Sprintf("k%d", i): "v"})
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < ops; i++ {
			m := s.Get(key)
			// Iterate the returned map — this is what real callers do.
			for k, v := range m {
				_ = k
				_ = v
			}
		}
	}()

	wg.Wait()
}

// TestEnvState_ConcurrentDeleteRacesAdd creates and deletes the same stage
// repeatedly while another goroutine adds to it. Tests that the inner-map
// initialization in Add() (the `if _, ok ... make(map)` branch) is safely
// re-entered after Delete() removes the key.
func TestEnvState_ConcurrentDeleteRacesAdd(t *testing.T) {
	s := &EnvState{env: make(map[string]map[string]string)}

	const key = "churn"
	const ops = 500
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < ops; i++ {
			s.Add(key, map[string]string{fmt.Sprintf("k%d", i): "v"})
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < ops; i++ {
			s.Delete(key)
		}
	}()

	wg.Wait()
}
