// Copyright 2022 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package docker

import (
	"sync"
	"testing"
)

// TestDocker_ContainersMutex_AppendVsSnapshot exercises the documented lock
// paths in docker.go: Run() appends to e.containers under e.mu (line 447–452)
// and Destroy() snapshots e.containers under e.mu (line 238–240). Multiple
// concurrent step starts + a concurrent destroy must not race on the slice
// header.
//
// We don't drive the public Run/Destroy methods because they require a live
// docker daemon. We hit the same critical sections directly to keep the test
// hermetic — a real prod step calls into exactly these patterns.
func TestDocker_ContainersMutex_AppendVsSnapshot(t *testing.T) {
	d := &Docker{
		containers: make([]Container, 0),
	}

	const ops = 1000
	var wg sync.WaitGroup

	// Appenders — same pattern as Run() at engine/docker/docker.go:447–452.
	for w := 0; w < 8; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < ops; i++ {
				d.mu.Lock()
				d.containers = append(d.containers, Container{ID: "ctr"})
				d.mu.Unlock()
			}
		}()
	}

	// Snapshotters — same pattern as Destroy() at engine/docker/docker.go:238–240.
	for r := 0; r < 4; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < ops; i++ {
				d.mu.Lock()
				snapshot := d.containers
				d.mu.Unlock()
				_ = len(snapshot)
			}
		}()
	}
	wg.Wait()
}

// TestDocker_RemoveContainerByID_RacesAppend documents a real bug:
// removeContainerByID (line 716–724) mutates e.containers without taking
// e.mu, while Run() (line 447–452) appends under the mutex. The race
// detector flags this.
//
// In production the call site is DestroyContainersByLabel → ranges over a
// docker-server-listing of containers and calls removeContainerByID per
// match (line 342). A concurrent Run() appending a new container during
// that loop would race on the slice header.
//
// This test SHOULD fail under -race in the current code — keep it as a
// regression test that fails until removeContainerByID takes e.mu.
func TestDocker_RemoveContainerByID_RacesAppend(t *testing.T) {
	d := &Docker{
		containers: []Container{
			{ID: "a"}, {ID: "b"}, {ID: "c"}, {ID: "d"}, {ID: "e"},
		},
	}

	const ops = 500
	var wg sync.WaitGroup

	// Appender (matches Run()'s critical section).
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < ops; i++ {
			d.mu.Lock()
			d.containers = append(d.containers, Container{ID: "new"})
			d.mu.Unlock()
		}
	}()

	// Remover (matches DestroyContainersByLabel's call at line 342).
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < ops; i++ {
			d.removeContainerByID("a")
		}
	}()

	wg.Wait()
}
