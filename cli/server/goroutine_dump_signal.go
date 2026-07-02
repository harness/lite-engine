// Copyright 2022 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

//go:build linux || darwin

package server

import (
	"context"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/sirupsen/logrus"
)

func startGoroutineDumpSignalHandler(ctx context.Context, dumpDir string) func() {
	signals := make(chan os.Signal, 1)
	stop := make(chan struct{})
	var stopOnce sync.Once

	signal.Notify(signals, syscall.SIGUSR1)
	go func() {
		for {
			select {
			case sig := <-signals:
				path, err := writeGoroutineDump(dumpDir, time.Now())
				if err != nil {
					logrus.WithError(err).Errorf("failed to write goroutine dump for signal: %s", sig)
					continue
				}
				logrus.Infof("received OS Signal %s; wrote goroutine dump to %s", sig, path)
			case <-ctx.Done():
				return
			case <-stop:
				return
			}
		}
	}()

	return func() {
		stopOnce.Do(func() {
			signal.Stop(signals)
			close(stop)
		})
	}
}
