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
	srv := &Server{log: log.New(logs, "", 0)}
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

func TestStatusLoggerIsSilentOnSuccessAndReportsEmpty(t *testing.T) {
	logs := &lockedBuf{}
	srv := &Server{log: log.New(logs, "", 0)}
	ok := &statusLogger{s: srv, where: "a"}
	ok.observe([]byte("HTTP/1.1 204 No Content\r\n\r\n"))
	ok.finish()
	if logs.String() != "" {
		t.Fatalf("a 2xx response was logged: %q", logs.String())
	}
	none := &statusLogger{s: srv, where: "b"}
	none.finish()
	if !strings.Contains(logs.String(), "upstream response b: empty") {
		t.Fatalf("logs = %q", logs.String())
	}
}
