// Copyright 2022 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package client

import (
	"context"
	"crypto/tls"
	"errors"
	"net/http/httptrace"
	"time"

	"github.com/harness/lite-engine/logger"
	"github.com/sirupsen/logrus"
)

type requestTrace struct {
	ConnReused bool
	LocalAddr  string
	RemoteAddr string
	DNSMS      int64
	ConnectMS  int64
	TLSMS      int64
	TTFBMS     int64
}

type tracedError struct {
	err   error
	trace requestTrace
}

func (e *tracedError) Error() string { return e.err.Error() }

func (e *tracedError) Unwrap() error { return e.err }

func wrapTracedError(err error, trace requestTrace) error {
	if err == nil {
		return nil
	}
	return &tracedError{err: err, trace: trace}
}

func traceFromError(err error) (requestTrace, bool) {
	var te *tracedError
	if errors.As(err, &te) {
		return te.trace, true
	}
	return requestTrace{}, false
}

type traceCollector struct {
	requestStart time.Time
	dnsStart     time.Time
	connectStart time.Time
	tlsStart     time.Time
	trace        requestTrace
}

func newTraceCollector() *traceCollector {
	return &traceCollector{requestStart: time.Now()}
}

func (tc *traceCollector) clientTrace() *httptrace.ClientTrace {
	return &httptrace.ClientTrace{
		DNSStart: func(_ httptrace.DNSStartInfo) {
			tc.dnsStart = time.Now()
		},
		DNSDone: func(_ httptrace.DNSDoneInfo) {
			if !tc.dnsStart.IsZero() {
				tc.trace.DNSMS = time.Since(tc.dnsStart).Milliseconds()
			}
		},
		GotConn: func(info httptrace.GotConnInfo) {
			tc.trace.ConnReused = info.Reused
			if info.Conn != nil {
				tc.trace.LocalAddr = info.Conn.LocalAddr().String()
				tc.trace.RemoteAddr = info.Conn.RemoteAddr().String()
			}
		},
		ConnectStart: func(_, _ string) {
			tc.connectStart = time.Now()
		},
		ConnectDone: func(_, _ string, _ error) {
			if !tc.connectStart.IsZero() {
				tc.trace.ConnectMS = time.Since(tc.connectStart).Milliseconds()
			}
		},
		TLSHandshakeStart: func() {
			tc.tlsStart = time.Now()
		},
		TLSHandshakeDone: func(_ tls.ConnectionState, _ error) {
			if !tc.tlsStart.IsZero() {
				tc.trace.TLSMS = time.Since(tc.tlsStart).Milliseconds()
			}
		},
		GotFirstResponseByte: func() {
			if tc.trace.TTFBMS == 0 {
				tc.trace.TTFBMS = time.Since(tc.requestStart).Milliseconds()
			}
		},
	}
}

func (tc *traceCollector) snapshot() requestTrace {
	return tc.trace
}

func logHTTPFailure(ctx context.Context, err error, method, path string) {
	fields := logrus.Fields{
		"method": method,
		"path":   path,
	}
	if tr, ok := traceFromError(err); ok {
		fields["conn_reused"] = tr.ConnReused
		if tr.LocalAddr != "" {
			fields["local_addr"] = tr.LocalAddr
		}
		if tr.RemoteAddr != "" {
			fields["remote_addr"] = tr.RemoteAddr
		}
		if tr.DNSMS > 0 {
			fields["dns_ms"] = tr.DNSMS
		}
		if tr.ConnectMS > 0 {
			fields["connect_ms"] = tr.ConnectMS
		}
		if tr.TLSMS > 0 {
			fields["tls_ms"] = tr.TLSMS
		}
		if tr.TTFBMS > 0 {
			fields["ttfb_ms"] = tr.TTFBMS
		}
	}

	logger.FromContext(ctx).WithError(err).WithFields(fields).Warn("lite-engine request failed")
}
