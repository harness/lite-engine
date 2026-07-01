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

	buf := make([]byte, n)
	copy(buf, data)

	l.mx.Lock()
	defer l.mx.Unlock()

	l.fullOutput.Write(buf)
	// Send the independent buf (not l.fullOutput.Bytes()) so a later Write that
	// grows/reallocates the buffer can't race a subscriber still reading. Keep
	// the <-l.done guard so a stuck/slow subscriber doesn't block the writer
	// forever once the step is done.
	for ch := range l.subscribers {
		select {
		case ch <- buf:
		case <-l.done:
			return n, nil
		}
	}

	return n, nil
}

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
