// Copyright 2022 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package runtime

import (
	"bytes"
	"context"
	"fmt"
	"sync"
)

type StepLog struct {
	mx          sync.Mutex
	fullOutput  *bytes.Buffer
	done        <-chan struct{}
	subscribers map[chan []byte]struct{}
}

func NewStepLog(ctx context.Context) *StepLog {
	l := &StepLog{
		mx:          sync.Mutex{},
		fullOutput:  &bytes.Buffer{},
		done:        ctx.Done(),
		subscribers: make(map[chan []byte]struct{}),
	}

	return l
}

func (l *StepLog) Done() <-chan struct{} {
	return l.done
}

// Write appends data to the full output buffer and fans it out to every
// active subscriber channel.
//
// We MUST send subscribers a slice whose backing array is independent of
// l.fullOutput. The previous implementation sent l.fullOutput.Bytes()[len-n:],
// which aliases the buffer's internal storage; the next Write could grow
// (and reallocate) that storage while a subscriber was still reading the
// prior slice — a real data race confirmed by `go test -race`. We allocate
// a fresh `buf`, write that into the buffer, and send that same independent
// slice to subscribers.
func (l *StepLog) Write(data []byte) (int, error) {
	n := len(data)

	buf := make([]byte, n)
	copy(buf, data)

	l.mx.Lock()
	l.fullOutput.Write(buf)
	for ch := range l.subscribers {
		ch <- buf
	}
	l.mx.Unlock()

	return n, nil
}

// Subscribe registers ch to receive further data output and returns the
// output log accumulated so far (from offset).
//
// The returned slice is a copy, not a reference to l.fullOutput's internal
// storage. A live reference would race with concurrent Write() calls
// growing the buffer.
func (l *StepLog) Subscribe(ch chan []byte, offset int) ([]byte, error) {
	l.mx.Lock()
	defer l.mx.Unlock()

	full := l.fullOutput.Bytes()
	if offset > len(full) {
		return nil, fmt.Errorf("error: index 'offset' is out of bounds Offset=%d Total=%d", offset, len(full))
	}
	out := make([]byte, len(full)-offset)
	copy(out, full[offset:])
	l.subscribers[ch] = struct{}{}
	return out, nil
}

func (l *StepLog) Unsubscribe(ch chan []byte) {
	l.mx.Lock()
	delete(l.subscribers, ch)
	l.mx.Unlock()
}
