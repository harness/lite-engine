// Copyright 2022 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package remote

import (
	"fmt"
	"time"
)

// Custom error.
type Error struct {
	Message string
	Code    int
}

func (e *Error) Error() string {
	return fmt.Sprintf("%d: %s", e.Code, e.Message)
}

// Link represents a signed link.
type Link struct {
	Value   string        `json:"link"`
	Expires time.Duration `json:"expires"`
}

// Line represents a line in the logs.
type Line struct {
	Level     string            `json:"level"`
	Number    int               `json:"pos"`
	Message   string            `json:"out"`
	Timestamp time.Time         `json:"time"`
	Args      map[string]string `json:"args"`
}
