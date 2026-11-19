// Copyright 2026 Harness Inc. All rights reserved.
// Use of this source code is governed by the PolyForm Free Trial 1.0.0 license
// that can be found in the licenses directory at the root of this repository, also available at
// https://polyformproject.org/wp-content/uploads/2020/05/PolyForm-Free-Trial-1.0.0.txt.

package sidecar

import (
	"log"
	"strings"
	"testing"
)

func TestStatusLoggerSkipsInterimResponses(t *testing.T) {
	logs := &lockedBuf{}
	srv := &session{Server: &Server{log: log.New(logs, "", 0)}, id: 7, source: "sidecar"}
	l := &statusLogger{s: srv, where: "example.com"}
	// The 100 and the 500 arrive split across reads.
	for _, chunk := range []string{"HTTP/1.1 100 Cont", "inue\r\n\r\nHTTP/1.1 5", "00 Internal Server Error\r\n\r\n"} {
		l.observe([]byte(chunk))
	}
	l.finish()
	got := logs.String()
	if strings.Contains(got, "100") || strings.Count(got, "500") != 1 || strings.Contains(got, "empty") {
		t.Fatalf("logs = %q", got)
	}
}

func TestStatusLoggerIsSilentOnNonErrorsAndReportsEmpty(t *testing.T) {
	logs := &lockedBuf{}
	srv := &session{Server: &Server{log: log.New(logs, "", 0)}, id: 7, source: "sidecar"}
	ok := &statusLogger{s: srv, where: "a"}
	ok.observe([]byte("HTTP/1.1 204 No Content\r\n\r\n"))
	ok.finish()
	redirect := &statusLogger{s: srv, where: "a"}
	redirect.observe([]byte("HTTP/1.1 301 Moved Permanently\r\n\r\n"))
	redirect.finish()
	if logs.String() != "" {
		t.Fatalf("a non-error response was logged: %q", logs.String())
	}
	none := &statusLogger{s: srv, where: "b"}
	none.finish()
	if !strings.Contains(logs.String(), "conn=7 upstream response b: empty") {
		t.Fatalf("logs = %q", logs.String())
	}
}

func TestStatusLoggerStopsOnOversizedInterimHead(t *testing.T) {
	logs := &lockedBuf{}
	l := &statusLogger{s: &session{Server: &Server{log: log.New(logs, "", 0)}}, where: "a"}
	l.observe([]byte("HTTP/1.1 100 Continue\r\n"))
	chunk := []byte(strings.Repeat("X-Pad: y\r\n", 1024))
	for i := 0; i < 16 && !l.logged; i++ {
		l.observe(chunk)
	}
	if !l.logged || l.pending != nil {
		t.Fatalf("logged=%v, %d bytes still buffered", l.logged, len(l.pending))
	}
}
