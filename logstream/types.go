package logstream

import "time"

type Line struct {
	Level       string
	Message     string
	ElaspedTime int64
	Number      int
	Timestamp   time.Time
}
