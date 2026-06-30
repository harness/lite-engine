// Copyright 2022 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

//go:build !(linux || darwin)

package server

import "context"

func startGoroutineDumpSignalHandler(context.Context, string) func() {
	return func() {}
}
