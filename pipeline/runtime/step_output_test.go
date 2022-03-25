// Copyright 2022 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package runtime

import (
	"bytes"
	"context"
	"testing"
	"time"
)

func TestStepLog(t *testing.T) { //nolint:gocyclo
	const (
		initDataPart1 = "Lorem ipsum dolor sit amet, "
		initDataPart2 = "consectetur adipiscing elit, "
		initData      = initDataPart1 + initDataPart2
		moreData      = "sed do eiusmod tempor incididunt ut labore et dolore magna aliqua."
	)

	ctx, cancelFn := context.WithCancel(context.Background())
	defer cancelFn()

	var n int
	var err error

	stepLog := NewStepLog(ctx)

	n, err = stepLog.Write([]byte(initDataPart1))
	if err != nil {
		t.Errorf("initial data write failed with error: %s", err.Error())
		return
	}
	if n != len(initDataPart1) {
		t.Errorf("initial data write failed, number of written bytes does not match, expected %d, but got %d", len(initDataPart1), n)
		return
	}

	n, err = stepLog.Write([]byte(initDataPart2))
	if err != nil {
		t.Errorf("initial data write failed with error: %s", err.Error())
		return
	}
	if n != len(initDataPart2) {
		t.Errorf("initial data write failed, number of written bytes does not match, expected %d, but got %d", len(initDataPart2), n)
		return
	}

	// wait for some time so that the written data get transferred to a buffer
	time.Sleep(10 * time.Millisecond)

	ch := make(chan []byte)
	oldData, err := stepLog.Subscribe(ch, 0)
	if err != nil {
		t.Errorf("failed to subscribe: %s", err.Error())
		return
	}

	defer stepLog.Unsubscribe(ch)

	if !bytes.Equal([]byte(initData), oldData) {
		t.Errorf("initial data doesn't match, expected %q but got %q", initData, oldData)
	}

	go func() {
		time.Sleep(100 * time.Millisecond)

		n, err := stepLog.Write([]byte(moreData))
		if err != nil {
			t.Errorf("data write failed with error: %s", err.Error())
			return
		}
		if n != len(moreData) {
			t.Errorf("data write failed, number of written bytes does not match, expected %d, but got %d", len(moreData), n)
			return
		}
	}()

	select {
	case <-stepLog.Done():
		t.Error("unexpected termination")
		return
	case data := <-ch:
		if !bytes.Equal([]byte(moreData), data) {
			t.Errorf("data doesn't match, expected %q but got %q", moreData, data)
			return
		}
	case <-time.After(time.Second):
		t.Error("failed to read data from subscription channel, timeout")
		return
	}

	go func() {
		time.Sleep(50 * time.Millisecond)
		cancelFn()
	}()

	select {
	case <-stepLog.Done():
	case <-ch:
		t.Error("unexpected data received")
		return
	case <-time.After(time.Second):
		t.Error("failed to wait for channel termination, timeout")
		return
	}
}

func TestStepLogStreaming(t *testing.T) {
	ctx, cancelFn := context.WithCancel(context.Background())

	const size = 10000

	stepLog := NewStepLog(ctx)

	// asynchronous process that writes bytes in regular intervals to step log
	go func() {
		defer cancelFn()

		data := new(byte)
		for i := 0; i < size; i++ {
			_, _ = stepLog.Write([]byte{*data})
			*data++
			time.Sleep(100 * time.Nanosecond)
		}
	}()

	// wait for some time so that the written data get transferred to a buffer
	time.Sleep(10 * time.Millisecond)

	ch := make(chan []byte)
	oldData, err := stepLog.Subscribe(ch, 0)
	if err != nil {
		t.Errorf("failed to subscribe: %s", err.Error())
		return
	}

	// async process that waits for the step log to finish and then closes the ch channel
	go func() {
		defer stepLog.Unsubscribe(ch)
		defer close(ch)

		select {
		case <-stepLog.Done():
			return
		case <-time.After(time.Second):
			t.Error("test timeout")
			return
		}
	}()

	buffer := bytes.NewBuffer(oldData)
	for data := range ch {
		buffer.Write(data)
	}

	if buffer.Len() != size {
		t.Errorf("buffer size mismatch, expected %d, but got %d", size, buffer.Len())
		return
	}

	var i uint16
	b := buffer.Bytes()
	for i = 0; i < size; i++ {
		if b[i] != byte(i&0xFF) {
			t.Errorf("buffer value mismatch at index %d, expected %x, but got %x", i, byte(i&0xFF), b[i])
		}
	}
}
