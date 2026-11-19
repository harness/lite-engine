// Copyright 2026 Harness Inc. All rights reserved.
// Use of this source code is governed by the PolyForm Free Trial 1.0.0 license
// that can be found in the licenses directory at the root of this repository, also available at
// https://polyformproject.org/wp-content/uploads/2020/05/PolyForm-Free-Trial-1.0.0.txt.

package sidecar

import (
	"errors"
	"net"
	"time"
)

const (
	httpPort           = 80
	tlsHandshakeRecord = 22
	tlsRecordHeaderLen = 5
	crlfLen            = 2
	blankLineLen       = 4

	copyBufferSize  = 32 * 1024
	readChunkSize   = 4096
	maxFirstRead    = 16 * 1024
	maxResponseHead = 64 * 1024
	maxStatusLine   = 512
	maxLogHost      = 80

	// bodyDrainGrace is how long exchange waits for the request-body goroutine
	// after the response has finished. The client connection is closed after
	// that, which ends the goroutine's read.
	bodyDrainGrace = 2 * time.Second
)

var (
	crlf     = []byte("\r\n")
	crlfcrlf = []byte("\r\n\r\n")

	errMalformedBody        = errors.New("malformed chunked request body")
	errResponseHeadTooLarge = errors.New("proxy response head too large")
)

func isTimeout(err error) bool {
	var ne net.Error
	return errors.As(err, &ne) && ne.Timeout()
}
