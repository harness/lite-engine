// Copyright 2026 Harness Inc. All rights reserved.
// Use of this source code is governed by the PolyForm Free Trial 1.0.0 license
// that can be found in the licenses directory at the root of this repository, also available at
// https://polyformproject.org/wp-content/uploads/2020/05/PolyForm-Free-Trial-1.0.0.txt.

package sidecar

import "testing"

// The sidecar parses bytes from any process on the VM, so these run the parsers
// on arbitrary input. Without -fuzz they run the seeds as ordinary tests.

// Splitting a body across two reads must not change what the parser decides.
func FuzzChunkParserSplitInvariance(f *testing.F) {
	f.Add([]byte("5\r\nhello\r\n0\r\n\r\n"), 3)
	f.Add([]byte("7fffffff\r\nx"), 1)
	f.Add([]byte("3\r\nabc\r\n0\r\nX-T: v\r\n\r\nGET /next"), 12)
	f.Fuzz(func(t *testing.T, b []byte, cut int) {
		if cut < 0 {
			cut = -cut
		}
		cut %= len(b) + 1

		var whole, split chunkParser
		nWhole, doneWhole := whole.consume(b)
		n1, _ := split.consume(b[:cut])
		n2, doneSplit := split.consume(b[cut:])

		if nWhole > len(b) || n1+n2 > len(b) {
			t.Fatal("consumed more bytes than were given")
		}
		if doneWhole != doneSplit || whole.malformed != split.malformed {
			t.Fatalf("split at %d changed the result: done %v/%v malformed %v/%v",
				cut, doneWhole, doneSplit, whole.malformed, split.malformed)
		}
		if doneWhole && !whole.malformed && nWhole != n1+n2 {
			t.Fatalf("split at %d changed the body length: %d vs %d", cut, nWhole, n1+n2)
		}
	})
}

// Whatever the bytes, a server name that is returned is safe to copy into a
// CONNECT line.
func FuzzSNIReturnsOnlyValidHostnames(f *testing.F) {
	f.Add(buildClientHello(f, "pypi.org"))
	f.Add(buildClientHello(f, ""))
	f.Fuzz(func(t *testing.T, b []byte) {
		if name := sniFromClientHello(b); name != "" && !validHostname(name) {
			t.Fatalf("returned an invalid host name %q", name)
		}
	})
}

// A request that rewrites successfully still has a head the proxy can parse,
// and no parser panics on arbitrary bytes.
func FuzzRequestParsing(f *testing.F) {
	f.Add([]byte("GET /x HTTP/1.1\r\nHost: a\r\n\r\nbody"))
	f.Add([]byte("CONNECT a:443 HTTP/1.1\r\nHost: a:443\r\n\r\n"))
	f.Fuzz(func(t *testing.T, b []byte) {
		for _, rewrite := range []func([]byte, string) ([]byte, bool, error){rewriteExplicit, rewriteAbsolute} {
			out, _, err := rewrite(b, "user:value")
			if err != nil {
				continue
			}
			if _, _, ok := splitHead(out); !ok {
				t.Fatalf("rewritten request has no end of head: %q", out)
			}
		}
		_ = looksLikeHTTP(b)
		_ = logHost(string(b))
		_, _ = contentLength(b)
		_ = isChunked(b)
	})
}
