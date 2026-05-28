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

func (l *StepLog) Write(data []byte) (int, error) {
	n := len(data)

	l.mx.Lock()

	l.fullOutput.Write(data)

	// fullOutput.Bytes() returns a slice that aliases the buffer's internal
	// storage. A subsequent fullOutput.Write may grow that storage and
	// realloc-copy it concurrently with subscribers reading the slice, which
	// is a data race (confirmed by go test -race in TestStepLogStreaming).
	// Send subscribers an independent copy of the just-written bytes so the
	// fan-out is decoupled from the buffer's lifetime.
	out := make([]byte, n)
	copy(out, l.fullOutput.Bytes()[l.fullOutput.Len()-n:])
	for ch := range l.subscribers {
		ch <- out
	}

	l.mx.Unlock()

	return n, nil
}

// Subscribe returns the output log that has been created so far (from the offset position) and
// it registers the ch channel to receive further data output.
func (l *StepLog) Subscribe(ch chan []byte, offset int) (data []byte, err error) {
	l.mx.Lock()
	// fullOutput.Bytes() aliases the buffer's internal storage. Returning that
	// slice to the caller while subsequent Write() calls grow the buffer is a
	// data race. Snapshot a copy under the lock so the caller owns its bytes.
	src := l.fullOutput.Bytes()
	snapshot := make([]byte, len(src))
	copy(snapshot, src)
	l.subscribers[ch] = struct{}{}
	l.mx.Unlock()

	if offset > len(snapshot) {
		err = fmt.Errorf("error: index 'offset' is out of bounds Offset=%d Total=%d", offset, len(snapshot))
		return
	}
	data = snapshot[offset:]
	return
}

func (l *StepLog) Unsubscribe(ch chan []byte) {
	l.mx.Lock()
	delete(l.subscribers, ch)
	l.mx.Unlock()
}
