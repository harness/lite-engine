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

	// replace byte buffer from which the data came before we write it to the subscriber channels
	data = l.fullOutput.Bytes()
	data = data[len(data)-n:]
	for ch := range l.subscribers {
		ch <- data
	}

	l.mx.Unlock()

	return n, nil
}

// Subscribe returns the output log that has been created so far (from the offset position) and
// it registers the ch channel to receive further data output.
func (l *StepLog) Subscribe(ch chan []byte, offset int) (data []byte, err error) {
	l.mx.Lock()
	data = l.fullOutput.Bytes()
	l.subscribers[ch] = struct{}{}
	l.mx.Unlock()

	if offset > len(data) {
		data = nil
		err = fmt.Errorf("error: index 'offset' is out of bounds Offset=%d Total=%d", offset, len(data))
	} else {
		data = data[offset:]
	}

	return
}

func (l *StepLog) Unsubscribe(ch chan []byte) {
	l.mx.Lock()
	delete(l.subscribers, ch)
	l.mx.Unlock()
}
