package sidecar

import (
	"bytes"
	"strconv"
	"strings"
)

// chunkParser walks a chunked body and reports how many bytes belong to it.
// Bytes after the terminating chunk are a pipelined request and are not part
// of this body.
type chunkParser struct {
	phase int // 0 size line, 1 chunk data, 2 trailers
	need  int
	line  []byte
	done  bool
}

func (p *chunkParser) consume(b []byte) (int, bool) {
	if p.done {
		return 0, true
	}
	n := 0
	for len(b) > 0 && !p.done {
		switch p.phase {
		case 0:
			nl := bytes.Index(b, crlf)
			if nl < 0 {
				p.line = append(p.line, b...)
				return n + len(b), false
			}
			p.line = append(p.line, b[:nl]...)
			size, ok := parseChunkSize(p.line)
			p.line = p.line[:0]
			b = b[nl+2:]
			n += nl + 2
			if !ok {
				p.done = true
				return n, true
			}
			if size == 0 {
				p.phase = 2
				continue
			}
			p.need = size + 2 // data plus the trailing CRLF
			p.phase = 1
		case 1:
			if len(b) < p.need {
				p.need -= len(b)
				return n + len(b), false
			}
			b = b[p.need:]
			n += p.need
			p.need = 0
			p.phase = 0
		case 2:
			combined := append(append([]byte{}, p.line...), b...)
			if bytes.HasPrefix(combined, crlf) {
				take := 2 - len(p.line)
				if take < 0 {
					take = 0
				}
				p.line = nil
				p.done = true
				return n + take, true
			}
			end := bytes.Index(combined, crlfcrlf)
			if end < 0 {
				p.line = combined
				return n + len(b), false
			}
			take := end + 4 - len(p.line)
			if take < 0 {
				take = 0
			}
			p.line = nil
			p.done = true
			return n + take, true
		}
	}
	return n, p.done
}

func parseChunkSize(line []byte) (int, bool) {
	if i := bytes.IndexByte(line, ';'); i >= 0 {
		line = line[:i]
	}
	line = bytes.TrimSpace(line)
	if len(line) == 0 {
		return 0, false
	}
	n, err := strconv.ParseInt(string(line), 16, 64)
	if err != nil || n < 0 {
		return 0, false
	}
	return int(n), true
}

func headerValue(head []byte, name string) (string, bool) {
	prefix := strings.ToLower(name) + ":"
	for _, line := range bytes.Split(head, crlf) {
		if strings.HasPrefix(strings.ToLower(string(line)), prefix) {
			return strings.TrimSpace(string(line[len(name)+1:])), true
		}
	}
	return "", false
}

func contentLength(head []byte) (int, bool) {
	raw, ok := headerValue(head, "Content-Length")
	if !ok {
		return 0, false
	}
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || n < 0 {
		return 0, false
	}
	return n, true
}

func chunked(head []byte) bool {
	raw, ok := headerValue(head, "Transfer-Encoding")
	return ok && strings.Contains(strings.ToLower(raw), "chunked")
}
