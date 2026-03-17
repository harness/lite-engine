// Copyright 2026 Harness Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package remote

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"syscall"
	"time"

	"github.com/cenkalti/backoff/v4"
	"github.com/sirupsen/logrus"

	"github.com/harness/lite-engine/logstream"
)

// ---------------------------------------------------------------------------
// Timeout & retry constants
// ---------------------------------------------------------------------------

const (
	// Transport-level timeouts

	// dialTimeout is the maximum time for establishing a TCP connection (includes DNS resolution).
	dialTimeout = 10 * time.Second
	// dialKeepAlive sends TCP keepalive probes at this interval to detect dead connections
	// caused by network issues, VM sleep/wake, or middlebox timeouts.
	dialKeepAlive = 30 * time.Second
	// tlsHandshakeTimeout fails fast on slow TLS handshakes instead of waiting for context deadline.
	tlsHandshakeTimeout = 10 * time.Second
	// responseHeaderTimeout is the maximum time to wait for the server's response headers.
	// This is the key timeout that prevents hanging on slow/unresponsive servers.
	responseHeaderTimeout = 10 * time.Second
	// idleConnTimeout proactively closes connections before typical server/load-balancer
	// timeouts (usually 120s), preventing EOF errors from reusing stale connections.
	idleConnTimeout = 90 * time.Second
	// expectContinueTimeout is the time to wait for a "100 Continue" response.
	expectContinueTimeout = 1 * time.Second
	// maxIdleConnsPerHost is increased from the default of 2 to better support concurrent
	// requests (log streaming, uploads) without exhausting the connection pool.
	maxIdleConnsPerHost = 20

	// Per-operation context timeouts (very conservative (highish) values)

	// openTimeout bounds the Open (POST) call that starts a log stream.
	openTimeout = 60 * time.Second
	// closeTimeout bounds the Close (DELETE) call that ends a log stream.
	closeTimeout = 60 * time.Second
	// writeTimeout bounds the Write (PUT) call used for periodic log flushes.
	writeTimeout = 60 * time.Second
	// uploadTimeout bounds the full-history Upload call (can be large).
	uploadTimeout = 120 * time.Second
	// uploadLinkTimeout bounds the call to fetch a signed upload link.
	uploadLinkTimeout = 60 * time.Second
	// uploadUsingLinkTimeout bounds the PUT of log data to the signed link.
	uploadUsingLinkTimeout = 120 * time.Second
)

// Compile-time check that OptimizedHTTPClient implements logstream.Client.
var _ logstream.Client = (*OptimizedHTTPClient)(nil)

// defaultOptimizedClient is the shared http.Client used when no custom TLS
// configuration (skipverify or mTLS) is required. Sharing a single client
// allows connection pooling across all OptimizedHTTPClient instances.
// nosemgrep: go.lang.security.audit.crypto.missing-ssl-minversion.missing-ssl-minversion
var defaultOptimizedClient = newHTTPClientWithTransport(&tls.Config{}) //nolint:gosec

// OptimizedHTTPClient provides an HTTP log-service client with proper timeouts,
// a well-configured transport, and retries only on transient errors.
type OptimizedHTTPClient struct {
	client         *http.Client
	endpoint       string // e.g. http://localhost:port
	token          string
	accountID      string
	indirectUpload bool
	customKafkaTopic string // when set, sent as X-Kafka-Topic on Write requests only
}

// NewOptimizedHTTPClient returns a new OptimizedHTTPClient.
// When customKafkaTopic is non-empty, Write requests include X-Kafka-Topic so log-service also pushes to that topic.
// When no custom TLS configuration is needed (skipverify=false and no mTLS
// certs), the shared defaultOptimizedClient is reused to benefit from
// connection pooling. A dedicated http.Client is created only when skipverify
// or mTLS is enabled.
func NewOptimizedHTTPClient(endpoint, accountID, token string, indirectUpload, skipverify bool, base64MtlsClientCert, base64MtlsClientCertKey string, customKafkaTopic string) *OptimizedHTTPClient {
	endpoint = strings.TrimSuffix(endpoint, "/")

	c := &OptimizedHTTPClient{
		client:            defaultOptimizedClient,
		endpoint:          endpoint,
		token:             token,
		accountID:         accountID,
		indirectUpload:    indirectUpload,
		customKafkaTopic:  customKafkaTopic,
	}

	// Load mTLS certificates if available
	mtlsEnabled, mtlsCerts := loadMTLSCerts(base64MtlsClientCert, base64MtlsClientCertKey)

	// Only create a dedicated http.Client when custom TLS is needed.
	if skipverify || mtlsEnabled {
		// nosemgrep: go.lang.security.audit.crypto.missing-ssl-minversion.missing-ssl-minversion
		tlsConfig := &tls.Config{
			InsecureSkipVerify: skipverify, //nolint:gosec
		}
		if mtlsEnabled {
			tlsConfig.Certificates = []tls.Certificate{mtlsCerts}
		}
		c.client = newHTTPClientWithTransport(tlsConfig)
	}

	return c
}

// newHTTPClientWithTransport creates an http.Client with an optimized transport
// configured for reliability and performance, particularly for environments with
// intermittent network issues (e.g., MacOS VMs with sleep/wake cycles, load
// balancers with idle timeouts).
func newHTTPClientWithTransport(tlsConfig *tls.Config) *http.Client {
	return &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
		Transport: &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			MaxIdleConnsPerHost:   maxIdleConnsPerHost,
			IdleConnTimeout:       idleConnTimeout,
			TLSHandshakeTimeout:   tlsHandshakeTimeout,
			ResponseHeaderTimeout: responseHeaderTimeout,
			ExpectContinueTimeout: expectContinueTimeout,
			DialContext: (&net.Dialer{
				Timeout:   dialTimeout,
				KeepAlive: dialKeepAlive,
			}).DialContext,
			TLSClientConfig: tlsConfig,
		},
	}
}

// ---------------------------------------------------------------------------
// logstream.Client implementation
// ---------------------------------------------------------------------------

// Open opens the data stream.
func (c *OptimizedHTTPClient) Open(ctx context.Context, key string) error {
	path := fmt.Sprintf(streamEndpoint, c.accountID, key)
	ctx, cancel := context.WithTimeout(ctx, openTimeout)
	defer cancel()
	b := newContextBackoff(ctx)
	return c.retry(ctx, c.endpoint+path, "POST", nil, nil, b, false)
}

// Close closes the data stream.
func (c *OptimizedHTTPClient) Close(ctx context.Context, key string, snapshot bool) error {
	ep := streamEndpoint
	if snapshot {
		ep = streamWithSnapshotEndpoint
	}
	path := fmt.Sprintf(ep, c.accountID, key)
	ctx, cancel := context.WithTimeout(ctx, closeTimeout)
	defer cancel()
	b := newContextBackoff(ctx)
	return c.retry(ctx, c.endpoint+path, "DELETE", nil, nil, b, false)
}

// Write writes logs to the data stream.
func (c *OptimizedHTTPClient) Write(ctx context.Context, key string, lines []*logstream.Line) error {
	path := fmt.Sprintf(streamEndpoint, c.accountID, key)
	l := convertLines(lines)
	ctx, cancel := context.WithTimeout(ctx, writeTimeout)
	defer cancel()
	b := newContextBackoff(ctx)
	return c.retry(ctx, c.endpoint+path, "PUT", &l, nil, b, true)
}

// Upload uploads the full log history to the data store or via log service.
// If indirectUpload is true, logs go through the log service instead of using
// a signed upload link.
func (c *OptimizedHTTPClient) Upload(ctx context.Context, key string, lines []*logstream.Line) error {
	data := new(bytes.Buffer)
	for _, line := range convertLines(lines) {
		buf := new(bytes.Buffer)
		if err := json.NewEncoder(buf).Encode(line); err != nil {
			logrus.WithError(err).WithField("key", key).
				Errorln("failed to encode line")
			return err
		}
		data.Write(buf.Bytes())
	}

	payload := data.Bytes()

	if c.indirectUpload {
		logrus.WithField("key", key).
			Infoln("uploading logs through log service as indirectUpload is specified as true")
		if err := c.uploadToRemoteStorage(ctx, key, payload); err != nil {
			logrus.WithError(err).WithField("key", key).
				Errorln("failed to upload logs through log service")
			return err
		}
		return nil
	}

	logrus.WithField("key", key).Infoln("calling upload link")
	link, err := c.uploadLink(ctx, key)
	if err != nil {
		logrus.WithError(err).WithField("key", key).
			Errorln("errored while trying to get upload link")
		return err
	}

	logrus.WithField("key", key).Infoln("uploading logs using link")
	if err := c.uploadUsingLink(ctx, link.Value, payload); err != nil {
		logrus.WithError(err).WithField("key", key).
			Errorln("failed to upload using link")
		return err
	}
	return nil
}

// ---------------------------------------------------------------------------
// Upload helpers
// ---------------------------------------------------------------------------

// uploadToRemoteStorage uploads log data directly to the blob endpoint.
func (c *OptimizedHTTPClient) uploadToRemoteStorage(ctx context.Context, key string, data []byte) error {
	path := fmt.Sprintf(blobEndpoint, c.accountID, key)
	ctx, cancel := context.WithTimeout(ctx, uploadTimeout)
	defer cancel()
	b := newContextBackoff(ctx)
	return c.retryOpen(ctx, c.endpoint+path, "POST", data, b)
}

// uploadLink returns a secure signed link for uploading to remote storage.
func (c *OptimizedHTTPClient) uploadLink(ctx context.Context, key string) (*Link, error) {
	path := fmt.Sprintf(uploadLinkEndpoint, c.accountID, key)
	out := new(Link)
	ctx, cancel := context.WithTimeout(ctx, uploadLinkTimeout)
	defer cancel()
	b := newContextBackoff(ctx)
	err := c.retry(ctx, c.endpoint+path, "POST", nil, out, b, false)
	return out, err
}

// uploadUsingLink uploads log data directly to the signed link.
func (c *OptimizedHTTPClient) uploadUsingLink(ctx context.Context, link string, data []byte) error {
	ctx, cancel := context.WithTimeout(ctx, uploadUsingLinkTimeout)
	defer cancel()
	b := newContextBackoff(ctx)
	return c.retryOpen(ctx, link, "PUT", data, b)
}

// ---------------------------------------------------------------------------
// Retry logic
// ---------------------------------------------------------------------------

// retry executes an HTTP request with retries on transient errors and 5xx
// responses. Non-transient errors (4xx, encoding failures, etc.) are returned
// immediately without retry. includeCustomKafkaTopic should be true only for Write (PUT /stream) requests.
func (c *OptimizedHTTPClient) retry(ctx context.Context, url, method string, in, out interface{}, b backoff.BackOff, includeCustomKafkaTopic bool) error {
	attempt := 0
	for {
		attempt++
		res, err := c.do(ctx, url, method, in, out, includeCustomKafkaTopic) //nolint:bodyclose

		if ctxErr := ctx.Err(); ctxErr != nil {
			logrus.WithError(ctxErr).WithField("url", url).
				Errorln("http: context canceled")
			return ctxErr
		}

		if !c.shouldRetry(res, err) {
			// Only log if there's an actual error (not for successful 2xx/3xx responses)
			if err != nil || (res != nil && res.StatusCode >= 400) {
				code := safeStatusCode(res)
				logrus.WithError(err).WithFields(logrus.Fields{
					"url": url, "method": method, "status": code, "attempt": attempt,
				}).Debugln("http: non-retryable error, returning immediately")
			}
			return err
		}

		wait := b.NextBackOff()
		if wait == backoff.Stop {
			logrus.WithError(err).WithFields(logrus.Fields{
				"url": url, "method": method, "attempts": attempt,
			}).Errorln("http: max retry limit reached")
			return err
		}

		code := safeStatusCode(res)
		logrus.WithError(err).WithFields(logrus.Fields{
			"url": url, "method": method, "attempt": attempt,
			"next_backoff": wait, "status": code,
		}).Warnln("http: retrying request")

		select {
		case <-time.After(wait):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// retryOpen is like retry but sends the body as a raw io.Reader (used for blob
// uploads where the body is not JSON-encoded).
func (c *OptimizedHTTPClient) retryOpen(ctx context.Context, url, method string, payload []byte, b backoff.BackOff) error {
	attempt := 0
	for {
		attempt++
		res, err := c.doRaw(ctx, url, method, bytes.NewReader(payload)) //nolint:bodyclose

		if ctxErr := ctx.Err(); ctxErr != nil {
			logrus.WithError(ctxErr).WithField("url", url).
				Errorln("http: context canceled")
			return ctxErr
		}

		if !c.shouldRetry(res, err) {
			// Only log if there's an actual error (not for successful 2xx/3xx responses)
			if err != nil || (res != nil && res.StatusCode >= 400) {
				code := safeStatusCode(res)
				logrus.WithError(err).WithFields(logrus.Fields{
					"url": url, "method": method, "status": code, "attempt": attempt,
				}).Debugln("http: non-retryable error, returning immediately")
			}
			return err
		}

		wait := b.NextBackOff()
		if wait == backoff.Stop {
			logrus.WithError(err).WithFields(logrus.Fields{
				"url": url, "method": method, "attempts": attempt,
			}).Errorln("http: max retry limit reached")
			return err
		}

		code := safeStatusCode(res)
		logrus.WithError(err).WithFields(logrus.Fields{
			"url": url, "method": method, "attempt": attempt,
			"next_backoff": wait, "status": code,
		}).Warnln("http: retrying request")

		select {
		case <-time.After(wait):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// shouldRetry returns true if the request should be retried based on the
// response status code or error type.
func (c *OptimizedHTTPClient) shouldRetry(res *http.Response, err error) bool {
	if err == nil && res != nil && res.StatusCode < 500 {
		return false
	}
	// Retry on 5xx server errors
	if res != nil && res.StatusCode >= 500 {
		return true
	}
	// Retry only on transient transport-level errors
	return isTransientError(err)
}

// ---------------------------------------------------------------------------
// HTTP helpers
// ---------------------------------------------------------------------------

// do executes an HTTP request with JSON encoding/decoding.
// includeCustomKafkaTopic should be true only for Write (PUT /stream) requests.
func (c *OptimizedHTTPClient) do(ctx context.Context, url, method string, in, out interface{}, includeCustomKafkaTopic bool) (*http.Response, error) {
	var r io.Reader
	if in != nil {
		buf := new(bytes.Buffer)
		if err := json.NewEncoder(buf).Encode(in); err != nil {
			logrus.WithError(err).Errorln("failed to encode input")
			return nil, err
		}
		r = buf
	}

	req, err := http.NewRequestWithContext(ctx, method, url, r)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Harness-Token", c.token)
	if includeCustomKafkaTopic && c.customKafkaTopic != "" {
		req.Header.Set(headerXKafkaTopic, c.customKafkaTopic)
	}

	res, err := c.client.Do(req)
	if res != nil {
		defer func() {
			// Drain the response body so we can reuse this connection.
			if _, cerr := io.Copy(io.Discard, io.LimitReader(res.Body, 4096)); cerr != nil { //nolint:mnd
				logrus.WithError(cerr).Errorln("failed to drain response body")
			}
			res.Body.Close()
		}()
	}
	if err != nil {
		return res, err
	}

	if res.StatusCode == http.StatusNoContent {
		return res, nil
	}

	body, err := io.ReadAll(res.Body)
	if err != nil {
		return res, err
	}

	if res.StatusCode > 299 { //nolint:mnd
		if len(body) != 0 {
			errObj := new(struct {
				Message string `json:"error_msg"`
			})
			if jsonErr := json.Unmarshal(body, errObj); jsonErr == nil {
				return res, &Error{Code: res.StatusCode, Message: errObj.Message}
			}
			return res, &Error{Code: res.StatusCode, Message: string(body)}
		}
		return res, errors.New(http.StatusText(res.StatusCode))
	}

	if out == nil {
		return res, nil
	}
	return res, json.Unmarshal(body, out)
}

// doRaw executes an HTTP request with a raw io.Reader body (no JSON encoding).
func (c *OptimizedHTTPClient) doRaw(ctx context.Context, url, method string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Harness-Token", c.token)

	res, err := c.client.Do(req)
	if res != nil {
		defer func() {
			if _, cerr := io.Copy(io.Discard, io.LimitReader(res.Body, 4096)); cerr != nil { //nolint:mnd
				logrus.WithError(cerr).Errorln("failed to drain response body")
			}
			res.Body.Close()
		}()
	}
	return res, err
}

// ---------------------------------------------------------------------------
// Transient error detection
// ---------------------------------------------------------------------------

// isTransientError returns true for errors that are safe to retry: connection
// resets, broken pipes, timeouts, EOF, network unreachable, etc.
func isTransientError(err error) bool {
	if err == nil {
		return false
	}
	// EOF errors indicate stale connections that were closed by the server/load balancer.
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}
	// Network timeouts (DNS resolution, connection establishment, TLS handshake, etc.)
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	// Connection-level errors that are transient:
	//  ECONNRESET  – connection reset by peer
	//  EPIPE       – broken pipe (wrote to closed connection)
	//  ECONNREFUSED – server not accepting connections
	//  ENETUNREACH  – network unreachable (routing issues)
	if errors.Is(err, syscall.ECONNRESET) ||
		errors.Is(err, syscall.EPIPE) ||
		errors.Is(err, syscall.ECONNREFUSED) ||
		errors.Is(err, syscall.ENETUNREACH) {
		return true
	}
	return false
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// newContextBackoff creates an exponential backoff bound to the given context.
// MaxElapsedTime is set to 0 (infinite) so the context timeout is the single
// source of truth for bounding the total retry duration.
func newContextBackoff(ctx context.Context) backoff.BackOff {
	exp := backoff.NewExponentialBackOff()
	exp.MaxElapsedTime = 0
	return backoff.WithContext(exp, ctx)
}

// safeStatusCode returns the HTTP status code or 0 if res is nil.
func safeStatusCode(res *http.Response) int {
	if res == nil {
		return 0
	}
	return res.StatusCode
}
