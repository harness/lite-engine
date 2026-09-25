// Copyright 2022 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package livelog

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/harness/lite-engine/duallog"
	"github.com/harness/lite-engine/internal/safego"
	"github.com/harness/lite-engine/logstream"
	"github.com/harness/lite-engine/logstream/remote"
	"github.com/harness/lite-engine/osstats"
	"github.com/sirupsen/logrus"
)

const (
	defaultInterval     = 1 * time.Second
	maxLineLimit        = 71680 // 70KB
	defaultLevel        = "info"
	defaultLimit        = 5242880 // 5MB
	flushThresholdTime  = 10 * time.Minute
	flushNetworkTimeout = 15 * time.Second

	// openWaitTimeout bounds how long Close() waits for the asynchronous Open() to
	// finish before the final flush. The stream is opened in a background goroutine
	// (see step_executor.go / common.go: safego "log_stream_open"), so a sub-second
	// step can reach Close() before the open completes; without waiting, flush() sees
	// !opened and drops the step's buffered lines from the live stream (they survive
	// only in the blob upload, which pure-ClickHouse reads never consult). Normal
	// steps never wait (open is already done); on timeout we proceed exactly as
	// before, so a slow/down log-service never blocks or fails a build. Kept below
	// Open()'s own RPC timeout so a down log-service never blocks a build for long.
	openWaitTimeout = 2 * time.Second
)

// Writer is an io.Writer that sends logs to the server.
type Writer struct {
	mu sync.Mutex

	client logstream.Client // client

	key  string // Unique key to identify in storage
	name string // Human readable name of the key

	// Whether to open the log stream in log-service.
	// This is useful for skipping the opening of the stream
	// in case it was already opened before by another service.
	skipOpeningStream bool // this param determine whether to skip opening the log stream in LE
	skipClosingStream bool // this param determine whether to skip closing the log stream in LE

	num    int
	now    time.Time
	size   int
	limit  int
	opened bool // whether the stream has been successfully opened
	nudges []logstream.Nudge
	errs   []error

	interval      atomic.Int64
	printToStdout bool // if logs should be written to both the log service and stdout
	pending       []*logstream.Line
	history       []*logstream.Line
	prev          []byte

	closed            bool
	close             chan struct{}
	ready             chan struct{}
	trimNewLineSuffix bool
	lastFlushTime     time.Time
	ctx               context.Context

	// openDone is closed once the asynchronous Open() attempt finishes (success or
	// failure). Close() waits on it briefly so a fast step cannot race ahead of the
	// open and lose its streamed logs. openOnce guards the close-exactly-once.
	openOnce sync.Once
	openDone chan struct{}

	dualLogMeta *duallog.Meta
	dualLogType string
}

// New returns a new writer
func New(ctx context.Context, client logstream.Client, key, name string, nudges []logstream.Nudge, printToStdout, trimNewLineSuffix, skipOpeningStream, skipClosingStream bool) *Writer {
	b := &Writer{
		client:            client,
		key:               key,
		name:              name,
		skipOpeningStream: skipOpeningStream,
		skipClosingStream: skipClosingStream,
		now:               time.Now(),
		printToStdout:     printToStdout,
		limit:             defaultLimit,
		nudges:            nudges,
		close:             make(chan struct{}),
		ready:             make(chan struct{}, 1),
		lastFlushTime:     time.Now(),
		trimNewLineSuffix: trimNewLineSuffix,
		ctx:               ctx,
		openDone:          make(chan struct{}),
	}
	b.interval.Store(int64(defaultInterval))
	safego.SafeGo("livelog_buffer", b.Start)
	return b
}

// SetLimit sets the Writer limit.
func (b *Writer) SetLimit(limit int) {
	b.mu.Lock()
	b.limit = limit
	b.mu.Unlock()
}

// SetInterval sets the Writer flusher interval.
func (b *Writer) SetInterval(interval time.Duration) {
	b.interval.Store(int64(interval))
}

// SetDualLogConfig enables dual logging to stdout in flat JSON format.
func (b *Writer) SetDualLogConfig(meta *duallog.Meta, logType string) {
	b.dualLogMeta = meta
	b.dualLogType = logType
}

// Write uploads the live log stream to the server.
func (b *Writer) Write(p []byte) (n int, err error) {
	// Return if a new line character is not present in the input.
	// Commands like `mvn` flush character by character so this prevents
	// spamming of single-character logs.
	if !bytes.Contains(p, []byte("\n")) {
		b.mu.Lock()
		b.prev = append(b.prev, p...)
		b.mu.Unlock()
		return len(p), nil
	}

	// Contains a new line. It may actually contain multiple new line characters
	// depending on the flushing logic. We find the index of the last \n and
	// add everything before it to res. Prev becomes whatever is left over.
	// Eg: Write(A)           ---> prev is A
	//     Write(BC\nDEF\nGH) ---> res becomes ABC\nDEF\n and prev becomes GH
	first, second := splitLast(p)

	b.mu.Lock()
	res := append(b.prev, first...) //nolint:gocritic // intentional: consume b.prev
	b.prev = second
	b.mu.Unlock()

	for _, part := range split(res) {
		if part == "" {
			continue
		}

		if b.trimNewLineSuffix {
			part = strings.TrimSuffix(part, "\n")
		}

		var (
			line       *logstream.Line
			marshalErr error
		)
		b.mu.Lock()
		line = &logstream.Line{
			Level:       defaultLevel,
			Message:     truncate(part, maxLineLimit),
			Number:      b.num,
			Timestamp:   time.Now(),
			ElaspedTime: int64(time.Since(b.now).Seconds()),
		}
		jsonLine, _ := getLineBytes(line)
		for b.size+len(jsonLine) > b.limit {
			if len(b.history) == 0 {
				break
			}
			hline, herr := getLineBytes(b.history[0])
			if herr != nil && marshalErr == nil {
				marshalErr = herr
			}
			b.size -= len(hline)
			b.history = b.history[1:]
		}
		b.size += len(jsonLine)
		b.num++
		closed := b.closed
		if !closed {
			b.pending = append(b.pending, line)
			b.history = append(b.history, line)
		}
		b.mu.Unlock()

		// logrus / duallog work happens AFTER the mutex is released to
		// preserve the "no logrus under b.mu" invariant.
		if b.dualLogMeta != nil {
			duallog.EmitLine(b.dualLogMeta, line.Message, line.Timestamp, b.dualLogType)
		}
		if b.printToStdout {
			logrus.WithField("name", b.name).Infoln(line.Message)
		}
		if marshalErr != nil {
			logrus.WithError(marshalErr).WithField("name", b.name).Errorln("could not marshal log")
		}
	}

	select {
	case b.ready <- struct{}{}:
	default:
	}

	return len(p), nil
}

func (b *Writer) Open() error {
	// Announce completion of the open attempt on every return path (success,
	// failure, or skip) so a step blocked in Close()'s waitForOpen is released.
	defer b.signalOpenDone()
	if b.skipOpeningStream {
		// Do nothing in case the stream has been already opened before.
		// Guard under mu: flush() checks b.opened under mu, so this write
		// must also be guarded to avoid a data race with the Start() flusher.
		b.mu.Lock()
		b.opened = true
		b.mu.Unlock()
		return nil
	}
	err := b.client.Open(b.ctx, b.key)
	if err != nil {
		logrus.WithError(err).WithField("key", b.key).
			Errorln("could not open the stream")
		b.stop() // stop trying to stream if we could not open the stream
		return err
	}
	// Set opened under mu before signaling openDone: flush() reads b.opened
	// under mu (see flush()), and the Start() flusher runs concurrently with
	// Open(), so an unguarded write here is a data race caught by -race.
	b.mu.Lock()
	b.opened = true
	b.mu.Unlock()
	logrus.WithField("name", b.name).Infoln("successfully opened log stream")
	return nil
}

// signalOpenDone closes openDone exactly once to announce that the asynchronous
// Open() attempt has finished (whether it succeeded or failed).
func (b *Writer) signalOpenDone() {
	b.openOnce.Do(func() {
		if b.openDone != nil {
			close(b.openDone)
		}
	})
}

// waitForOpen blocks until the asynchronous Open() has finished or timeout elapses.
// It is the fix for the fast-step log-loss race: a sub-second step can reach Close()
// before the "log_stream_open" goroutine has returned, and without this wait the
// final flush would see !opened and drop the step's buffered lines from the live
// (ClickHouse) stream. The wait is non-fatal and self-limiting: a normal step
// returns instantly (openDone already closed), a fast step waits only until the
// quick open RPC lands, and a down/slow log-service hits the timer and proceeds.
//
// It keys off the openDone channel rather than reading b.opened directly: Open()
// mutates b.opened without holding b.mu, so an unsynchronized read here would race
// (go test -race). Channel close/receive gives the necessary happens-before edge.
func (b *Writer) waitForOpen(timeout time.Duration) {
	if b.openDone == nil {
		return
	}
	// Fast path: open already finished (channel closed) — return immediately, so a
	// normal step pays nothing.
	select {
	case <-b.openDone:
		return
	default:
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-b.openDone:
	case <-timer.C:
		logrus.WithField("key", b.key).Warnln("timed out waiting for log stream open before close; proceeding")
	}
}

// Close closes the writer and uploads the full contents to
// the server.
func (b *Writer) Close() error {
	// Give the asynchronous Open() a bounded, non-fatal moment to finish before we
	// flush. Without this a sub-second step races ahead of the open and flush() drops
	// its buffered logs (they would only reach the blob upload, not the live stream).
	b.waitForOpen(openWaitTimeout)
	if b.skipClosingStream {
		return b.writeWithoutClose()
	}
	// Drain the trailing, newline-less line into pending BEFORE stopping the stream.
	// Write() only appends to pending/history while the stream is open (!closed); if
	// we stop first, this final line — frequently the most important one, e.g. an
	// error or exit summary emitted with printf/echo -n and no trailing newline —
	// reaches neither the live stream nor the blob and is silently lost. Under pure
	// ClickHouse there is no blob fallback, so it must be streamed here.
	b.mu.Lock()
	hasPrev := len(b.prev) > 0
	b.mu.Unlock()
	if hasPrev {
		b.Write([]byte("\n")) //nolint:errcheck
	}
	if b.stop() {
		b.flush()
	}

	b.checkErrInLogs()

	var err error
	if !b.skipOpeningStream {
		// In case skipOpeningStream is `true` it means the log stream
		// was already opened before by some other class or service.
		// In this case, we should not call the blob-upload endpoint here,
		// as it would delete logs previously sent by the other class or service.
		// TODO: We can get rid of this logic by implementing a "append" parameter
		//       in the log-service `upload` API. Then we can just call `upload`
		//       with `append=true` here, and previously written logs will be kept.
		err = b.upload()
		if err != nil {
			logrus.WithError(err).WithField("key", b.key).
				Errorln("failed to upload logs")
		}
	}

	// Close the log stream once upload has completed. Log in case of any error

	if errc := b.client.Close(b.ctx, b.key, b.skipOpeningStream); errc != nil {
		// In case skipOpeningStream is true, we call the stream-close endpoint passing
		// `snapshot=true`, which will close the stream and snapshot its content into a blob.
		// TODO: we can get rid of using `snapshot=true` here once we are able to append logs
		//       to blob via log-service.
		logrus.WithError(errc).WithField("key", b.key).
			Errorln("failed to close log stream")
	}
	logrus.WithField("name", b.name).Infoln("successfully closed log stream")
	return err
}

func (b *Writer) writeWithoutClose() error {
	b.mu.Lock()
	hasPrev := len(b.prev) > 0
	b.mu.Unlock()
	if hasPrev {
		b.Write([]byte("\n")) //nolint:errcheck
	}
	err := b.flush()
	if err != nil {
		logrus.WithError(err).WithField("key", b.key).
			Errorln("failed to flush the stream")
	}
	b.checkErrInLogs()
	return nil
}

// upload uploads the full log history to the server.
func (b *Writer) upload() error {
	return b.client.Upload(b.ctx, b.key, b.history)
}

// Flush sends any buffered log lines to the stream. Call after writing the final
// summary line so it is included before Close() uploads and closes the stream.
func (b *Writer) Flush() error {
	return b.flush()
}

// flush batch uploads all buffered logs to the server.

func (b *Writer) flush() error {
	// Check b.opened under mu: Open() sets it under mu and Start()'s flush
	// loop runs concurrently with Open(), so an unguarded read is a data race.
	b.mu.Lock()
	if !b.opened {
		b.mu.Unlock()
		return nil
	}
	lines := b.copy()
	b.clear()

	idleTooLong := len(lines) == 0 && b.lastFlushTime.Before(time.Now().Add(-flushThresholdTime))
	if len(lines) > 0 || idleTooLong {
		b.lastFlushTime = time.Now()
	}
	b.mu.Unlock()

	if len(lines) == 0 {
		if idleTooLong {
			// DumpProcessInfo logs via logrus; must run outside b.mu so the
			// StreamHook (if registered) doesn't re-enter Writer.Write.
			if err := osstats.DumpProcessInfo(); err != nil {
				log.Printf("failed to dump process info: %v", err)
			}
		}
		return nil
	}

	ctx, cancel := context.WithTimeout(b.ctx, flushNetworkTimeout)
	defer cancel()
	err := b.client.Write(ctx, b.key, lines)
	if err != nil {
		log.Printf("failed to flush lines: key=%s num_lines=%d err=%v", b.key, len(lines), err)
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

// Start starts a periodic loop to flush logs to the live stream
func (b *Writer) Start() {
	intervalTimer := time.NewTimer(time.Duration(b.interval.Load()))
	for {
		select {
		case <-b.close:
			return
		case <-b.ready:
			intervalTimer.Reset(time.Duration(b.interval.Load()))
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
	for idx := max(0, size-10); idx < size; idx++ { //nolint:mnd
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

func max(a, b int) int { //nolint:gocritic,revive // builtinShadowDecl,redefines-builtin-id: intentional helper function name
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
