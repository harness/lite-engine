// Copyright 2022 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package logstream

import "time"

type Line struct {
	Level       string
	Message     string
	ElaspedTime int64
	Number      int
	Timestamp   time.Time
}
