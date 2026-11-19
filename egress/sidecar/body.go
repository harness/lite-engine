// Copyright 2026 Harness Inc. All rights reserved.
// Use of this source code is governed by the PolyForm Free Trial 1.0.0 license
// that can be found in the licenses directory at the root of this repository, also available at
// https://polyformproject.org/wp-content/uploads/2020/05/PolyForm-Free-Trial-1.0.0.txt.

package sidecar

import (
	"bytes"
	"math"
	"strconv"
)

const (
	phaseSize = iota
	phaseData
	phaseTrailer

	// maxChunkLine caps a chunk-size line or a trailer line. A longer line is
	// treated as a malformed body instead of growing without bound.
	maxChunkLine = 4096
	// maxChunkSize keeps size plus the trailing CRLF inside an int on every
	// platform, so a hostile size cannot overflow.
	maxChunkSize = math.MaxInt32 - crlfLen
)

// chunkParser walks a chunked request body and reports how many bytes belong
// to it. Bytes after the terminating chunk are a pipelined request and are not
// part of this body.
type chunkParser struct {
	phase     int
	need      int // bytes left in the current chunk, including its CRLF
	line      []byte
	done      bool
	malformed bool
}

// consume returns how many bytes of b belong to the body and whether the body
// is complete. After a malformed body, done is true and malformed is set.
func (p *chunkParser) consume(b []byte) (used int, done bool) {
	for len(b) > 0 && !p.done {
		var n int
		switch p.phase {
		case phaseSize:
			n = p.consumeSizeLine(b)
		case phaseData:
			n = p.consumeData(b)
		default:
			n = p.consumeTrailer(b)
		}
		used += n
		b = b[n:]
	}
	return used, p.done
}

func (p *chunkParser) fail() {
	p.done = true
	p.malformed = true
}

// nextLine accumulates bytes until a CRLF, so a CRLF split across two reads is
// still found. It returns the line without the CRLF, how many bytes of b it
// used, and whether a full line was found.
func (p *chunkParser) nextLine(b []byte) (line []byte, used int, found bool) {
	prev := len(p.line)
	p.line = append(p.line, b...)
	nl := bytes.Index(p.line, crlf)
	if nl < 0 {
		if len(p.line) > maxChunkLine {
			p.fail()
		}
		return nil, len(b), false
	}
	line = append([]byte(nil), p.line[:nl]...)
	p.line = p.line[:0]
	return line, nl + crlfLen - prev, true
}

func (p *chunkParser) consumeSizeLine(b []byte) int {
	line, used, found := p.nextLine(b)
	if !found {
		return used
	}
	size, ok := parseChunkSize(line)
	switch {
	case !ok:
		p.fail()
	case size == 0:
		p.phase = phaseTrailer
	default:
		p.need = size + crlfLen
		p.phase = phaseData
	}
	return used
}

func (p *chunkParser) consumeData(b []byte) int {
	if len(b) < p.need {
		p.need -= len(b)
		return len(b)
	}
	used := p.need
	p.need = 0
	p.phase = phaseSize
	return used
}

func (p *chunkParser) consumeTrailer(b []byte) int {
	line, used, found := p.nextLine(b)
	if found && len(line) == 0 {
		p.done = true
	}
	return used
}

func parseChunkSize(line []byte) (int, bool) {
	if i := bytes.IndexByte(line, ';'); i >= 0 {
		line = line[:i]
	}
	line = bytes.TrimSpace(line)
	if len(line) == 0 {
		return 0, false
	}
	n, err := strconv.ParseInt(string(line), 16, 32)
	if err != nil || n < 0 || n > maxChunkSize {
		return 0, false
	}
	return int(n), true
}
