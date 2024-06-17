// Copyright 2022 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package livelog

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/harness/lite-engine/logstream"
	"github.com/harness/lite-engine/logstream/remote"
	"github.com/harness/lite-engine/osstats"
)

const (
	defaultInterval    = 1 * time.Second
	maxLineLimit       = 2048 // 2KB
	defaultLevel       = "info"
	defaultLimit       = 5242880 // 5MB
	flushThresholdTime = 10 * time.Minute
)

// Writer is an io.Writer that sends logs to the server.
type Writer struct {
	mu sync.Mutex

	client logstream.Client // client

	key  string // Unique key to identify in storage
	name string // Human readable name of the key

	num    int
	now    time.Time
	size   int
	limit  int
	opened bool // whether the stream has been successfully opened
	nudges []logstream.Nudge
	errs   []error

	interval      time.Duration
	printToStdout bool // if logs should be written to both the log service and stdout
	pending       []*logstream.Line
	history       []*logstream.Line
	prev          []byte

	closed bool
	close  chan struct{}
	ready  chan struct{}

	lastFlushTime time.Time
}

// New returns a new writer
func New(client logstream.Client, key, name string, nudges []logstream.Nudge, printToStdout bool) *Writer {
	b := &Writer{
		client:        client,
		key:           key,
		name:          name,
		now:           time.Now(),
		printToStdout: printToStdout,
		limit:         defaultLimit,
		interval:      defaultInterval,
		nudges:        nudges,
		close:         make(chan struct{}),
		ready:         make(chan struct{}, 1),
		lastFlushTime: time.Now(),
	}
	go b.Start()
	return b
}

// SetLimit sets the Writer limit.
func (b *Writer) SetLimit(limit int) {
	b.limit = limit
}

// SetInterval sets the Writer flusher interval.
func (b *Writer) SetInterval(interval time.Duration) {
	b.interval = interval
}

// Write uploads the live log stream to the server.
func (b *Writer) Write(p []byte) (n int, err error) {
	var res []byte
	// Return if a new line character is not present in the input.
	// Commands like `mvn` flush character by character so this prevents
	// spamming of single-character logs.
	if !bytes.Contains(p, []byte("\n")) {
		b.prev = append(b.prev, p...)
		return len(p), nil
	}

	// Contains a new line. It may actually contain multiple new line characters
	// depending on the flushing logic. We find the index of the last \n and
	// add everything before it to res. Prev becomes whatever is left over.
	// Eg: Write(A)           ---> prev is A
	//     Write(BC\nDEF\nGH) ---> res becomes ABC\nDEF\n and prev becomes GH
	first, second := splitLast(p)

	res = b.prev
	res = append(res, first...)
	b.prev = second

	for _, part := range split(res) {
		if part == "" {
			continue
		}
		line := &logstream.Line{
			Level:       defaultLevel,
			Message:     truncate(part, maxLineLimit),
			Number:      b.num,
			Timestamp:   time.Now(),
			ElaspedTime: int64(time.Since(b.now).Seconds()),
		}

		jsonLine, _ := getLineBytes(line)

		if b.printToStdout {
			logrus.WithField("name", b.name).Infoln(line.Message)
		}

		for b.size+len(jsonLine) > b.limit {
			// Keep streaming even after the limit, but only upload last `b.limit` data to the store
			if len(b.history) == 0 {
				break
			}

			hline, err := getLineBytes(b.history[0])
			if err != nil {
				logrus.WithError(err).WithField("name", b.name).Errorln("could not marshal log")
			}
			b.size -= len(hline)
			b.history = b.history[1:]
		}

		b.size += len(jsonLine)
		b.num++

		if !b.stopped() {
			b.mu.Lock()
			b.pending = append(b.pending, line)
			b.mu.Unlock()
		}

		b.mu.Lock()
		b.history = append(b.history, line)
		b.mu.Unlock()
	}

	select {
	case b.ready <- struct{}{}:
	default:
	}

	return len(p), nil
}

func (b *Writer) Open() error {
	err := b.client.Open(context.Background(), b.key)
	if err != nil {
		logrus.WithError(err).WithField("key", b.key).
			Errorln("could not open the stream")
		b.stop() // stop trying to stream if we could not open the stream
		return err
	}
	logrus.WithField("name", b.name).Infoln("successfully opened log stream")
	b.opened = true
	return nil
}

// Close closes the writer and uploads the full contents to
// the server.
func (b *Writer) Close() error {
	if b.stop() {
		// Flush anything waiting on a new line
		if len(b.prev) > 0 {
			b.Write([]byte("\n")) //nolint:errcheck
		}
		b.flush()
	}

	b.checkErrInLogs()

	err := b.upload()
	if err != nil {
		logrus.WithError(err).WithField("key", b.key).
			Errorln("failed to upload logs")
	}
	// Close the log stream once upload has completed. Log in case of any error

	if errc := b.client.Close(context.Background(), b.key); errc != nil {
		logrus.WithError(errc).WithField("key", b.key).
			Errorln("failed to close log stream")
	}
	logrus.WithField("name", b.name).Infoln("successfully closed log stream")
	return err
}

// upload uploads the full log history to the server.
func (b *Writer) upload() error {
	return b.client.Upload(context.Background(), b.key, b.history)
}

// flush batch uploads all buffered logs to the server.
func (b *Writer) flush() error {
	if !b.opened {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	lines := b.copy()
	b.clear()
	if len(lines) == 0 {
		thresholdTime := time.Now().Add(-flushThresholdTime)
		if b.lastFlushTime.Before(thresholdTime) {
			osstats.DumpProcessInfo()
		}
		b.lastFlushTime = time.Now()
		return nil
	}
	b.lastFlushTime = time.Now()
	err := b.client.Write(context.Background(), b.key, lines)
	if err != nil {
		logrus.WithError(err).WithField("key", b.key).WithField("num_lines", len(lines)).
			Errorln("failed to flush lines")
		return err
	}
	return nil
}

func (b *Writer) Error() error {
	if len(b.errs) == 0 {
		return nil
	}
	return b.errs[len(b.errs)-1]
}

// copy returns a copy of the buffered lines.
func (b *Writer) copy() []*logstream.Line {
	return append(b.pending[:0:0], b.pending...)
}

// clear clears the buffer.
func (b *Writer) clear() {
	b.pending = b.pending[:0]
}

func (b *Writer) stop() bool {
	b.mu.Lock()
	var closed bool
	if !b.closed {
		close(b.close)
		closed = true
		b.closed = true
	}
	b.mu.Unlock()
	return closed
}

func (b *Writer) stopped() bool {
	b.mu.Lock()
	closed := b.closed
	b.mu.Unlock()
	return closed
}

// Start starts a periodic loop to flush logs to the live stream
func (b *Writer) Start() {
	intervalTimer := time.NewTimer(b.interval)
	for {
		select {
		case <-b.close:
			return
		case <-b.ready:
			intervalTimer.Reset(b.interval)
			select {
			case <-b.close:
				return
			case <-intervalTimer.C:
				// we intentionally ignore errors. log streams
				// are ephemeral and are considered low priority
				err := b.flush()
				// Write the error to help with debugging
				if err != nil {
					logrus.WithField("key", b.key).WithError(err).
						Errorln("errored while trying to flush lines")
				}
			}
		}
	}
}

func (b *Writer) checkErrInLogs() {
	size := len(b.history)
	// Check last 10 log lines for errors. TODO(Shubham): see if this can be made better
	for idx := max(0, size-10); idx < size; idx++ { //nolint:gomnd
		line := b.history[idx]
		// Iterate over the nudges and see if we get a match
		for _, n := range b.nudges {
			r, err := regexp.Compile(n.GetSearch())
			if err != nil {
				logrus.WithError(err).WithField("key", b.key).Errorln("error while compiling regex")
				continue
			}
			if r.MatchString(line.Message) {
				b.errs = append(b.errs, formatNudge(line, n))
			}
		}
	}
}

func getLineBytes(line *logstream.Line) ([]byte, error) {
	remoteLine := remote.ConvertToRemote(line)
	jsonline, err := json.Marshal(remoteLine)
	if err != nil {
		return jsonline, err
	}
	jsonline = append(jsonline, []byte("\n")...)
	return jsonline, err
}

// return back two byte arrays after splitting on last \n.
// Eg: ABC\nDEF\nGH will return ABC\nDEF\n and GH
func splitLast(p []byte) ([]byte, []byte) { //nolint:gocritic
	if !bytes.Contains(p, []byte("\n")) {
		return p, []byte{} // If no \n is present, return the string itself
	}
	s := string(p)
	last := strings.LastIndex(s, "\n")
	first := s[:last+1]
	second := s[last+1:]
	return []byte(first), []byte(second)
}

func split(p []byte) []string {
	s := string(p)
	v := []string{s}
	// kubernetes buffers the output and may combine
	// multiple lines into a single block of output.
	// Split into multiple lines.
	//
	// note that docker output always inclines a line
	// feed marker. This needs to be accounted for when
	// splitting the output into multiple lines.
	if strings.Contains(strings.TrimSuffix(s, "\n"), "\n") {
		v = strings.SplitAfter(s, "\n")
	}
	return v
}

func formatNudge(line *logstream.Line, nudge logstream.Nudge) error {
	return fmt.Errorf("found possible error on line %d.\n Log: %s.\n Possible error: %s.\n Possible resolution: %s",
		line.Number+1, line.Message, nudge.GetError(), nudge.GetResolution())
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// truncates a string to the given length
func truncate(inp string, to int) string {
	if len(inp) > to {
		return inp[:to] + "... (log line truncated)"
	}
	return inp
}
