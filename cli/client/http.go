// Copyright 2022 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package client

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptrace"
	"sync"
	"time"

	"github.com/harness/lite-engine/api"
	"github.com/harness/lite-engine/logger"
	"github.com/sirupsen/logrus"
	"golang.org/x/net/http2"
)

// Connectivity tuning for the runner -> lite-engine path. These bound the worst
// case for a stalled call so the caller retries (on a fresh connection) instead
// of black-holing for the whole request budget.
//
// IMPORTANT: start_step is fast/async server-side, but poll_step is a LONG-POLL
// (it blocks until the step finishes, up to hours) and setup is synchronous and
// variable-duration. So we deliberately do NOT cap time-to-response at the
// transport level (no ResponseHeaderTimeout) — that would kill long polls. Dead
// or black-holed connections are instead detected by HTTP/2 keepalive pings,
// which a slow-but-alive server (including a long-poll) always answers.
const (
	// dialTimeout bounds TCP connect. On a private VPC a healthy connect is
	// sub-second; a black-holed SYN must fail fast so the caller can retry.
	dialTimeout = 10 * time.Second
	// http2ReadIdleTimeout / http2PingTimeout enable HTTP/2 keepalive pings so
	// a dead (black-holed) connection is detected and torn down even mid-call.
	// A slow-but-alive server (incl. a long-poll) answers the PING at the
	// transport layer, so legitimately long calls are unaffected.
	http2ReadIdleTimeout = 10 * time.Second
	http2PingTimeout     = 5 * time.Second
	// phaseLogSlowThreshold is the connection-establishment latency (TCP+TLS,
	// not total elapsed) above which a successful call still gets a phase-trace
	// WARN. Measuring connect/TLS only means a legitimately long poll/setup does
	// not spam warnings, while a slow or black-holing connect is still surfaced.
	phaseLogSlowThreshold = 8 * time.Second
	// startStepAttemptTimeout caps a single start_step attempt so the overall
	// retry budget is spent on multiple real attempts (each a fresh connection,
	// which can route around a per-flow black-hole) instead of one blocking
	// call. start_step is async server-side, so a healthy attempt is well under
	// this. NOTE: only start_step is capped this way; setup and poll are not
	// (see RetrySetup / RetryPollStep for why).
	startStepAttemptTimeout = 12 * time.Second
)

var (
	healthCheckTimeout = 10 * time.Second
)

// attemptContext derives a per-attempt context from the overall retry context,
// capped at perAttempt but never exceeding the remaining overall budget. This is
// what lets a single black-holed call be abandoned and retried within budget.
func attemptContext(retryCtx context.Context, perAttempt time.Duration) (context.Context, context.CancelFunc) {
	d := perAttempt
	if dl, ok := retryCtx.Deadline(); ok {
		if remaining := time.Until(dl); remaining < d {
			d = remaining
		}
	}
	return context.WithTimeout(retryCtx, d)
}

// httpPhases captures per-phase wall-clock offsets (ms from request start) for a
// single outbound HTTP call so we can tell apart a connection-establishment hang
// (network black-hole) from a server-side stall (no response headers).
type httpPhases struct {
	mu           sync.Mutex
	start        time.Time
	getConn      int64
	connStart    int64
	connDone     int64
	connErr      string
	tlsStart     int64
	tlsDone      int64
	wroteReq     int64
	gotFirstByte int64
	reusedConn   bool
}

func (p *httpPhases) ms() int64 { return time.Since(p.start).Milliseconds() }

// connectEstablishMs returns the ms spent establishing the connection (TCP, plus
// TLS when present). For a reused connection it is ~0. It is used to decide
// whether a *successful* call was slow because of connection setup (suspicious,
// worth a WARN) rather than server response time (legitimate for a long-poll).
func (p *httpPhases) connectEstablishMs() int64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.tlsDone > 0 {
		return p.tlsDone
	}
	return p.connDone
}

func (p *httpPhases) trace() *httptrace.ClientTrace {
	return &httptrace.ClientTrace{
		GetConn:      func(string) { p.mu.Lock(); p.getConn = p.ms(); p.mu.Unlock() },
		ConnectStart: func(_, _ string) { p.mu.Lock(); p.connStart = p.ms(); p.mu.Unlock() },
		ConnectDone: func(_, _ string, err error) {
			p.mu.Lock()
			p.connDone = p.ms()
			if err != nil {
				p.connErr = err.Error()
			}
			p.mu.Unlock()
		},
		TLSHandshakeStart:    func() { p.mu.Lock(); p.tlsStart = p.ms(); p.mu.Unlock() },
		TLSHandshakeDone:     func(tls.ConnectionState, error) { p.mu.Lock(); p.tlsDone = p.ms(); p.mu.Unlock() },
		GotConn:              func(i httptrace.GotConnInfo) { p.mu.Lock(); p.reusedConn = i.Reused; p.mu.Unlock() },
		WroteRequest:         func(httptrace.WroteRequestInfo) { p.mu.Lock(); p.wroteReq = p.ms(); p.mu.Unlock() },
		GotFirstResponseByte: func() { p.mu.Lock(); p.gotFirstByte = p.ms(); p.mu.Unlock() },
	}
}

// fields returns the captured phase offsets plus a derived "last_phase" naming
// the furthest point the request reached before it failed/returned. This is the
// signal that classifies a failure as network (connect_awaiting_synack) vs
// server-side stall (wrote_request_awaiting_response) without VPC/VM logs.
func (p *httpPhases) fields() map[string]interface{} {
	p.mu.Lock()
	defer p.mu.Unlock()
	last := "got_conn"
	switch {
	case p.gotFirstByte > 0:
		last = "got_first_response_byte"
	case p.wroteReq > 0:
		last = "wrote_request_awaiting_response" // connected+sent, server silent => server-side stall
	case p.tlsDone > 0:
		last = "tls_done"
	case p.tlsStart > 0:
		last = "tls_handshake_awaiting" // TLS started, no completion
	case p.connDone > 0:
		last = "connect_done"
	case p.connStart > 0:
		last = "connect_awaiting_synack" // SYN sent, no SYN-ACK => network black-hole
	case p.getConn > 0:
		last = "acquiring_conn"
	}
	return map[string]interface{}{
		"last_phase":       last,
		"reused_conn":      p.reusedConn,
		"ms_get_conn":      p.getConn,
		"ms_connect_start": p.connStart,
		"ms_connect_done":  p.connDone,
		"connect_err":      p.connErr,
		"ms_tls_start":     p.tlsStart,
		"ms_tls_done":      p.tlsDone,
		"ms_wrote_request": p.wroteReq,
		"ms_first_byte":    p.gotFirstByte,
	}
}

// logPhaseTrace emits a structured WARN with the request's network phase timings
// whenever a call fails, or succeeds but spent too long establishing the
// connection. It stays silent for healthy calls (and for legitimately long
// poll_step/setup calls, whose connect is fast) so the high-frequency poll/
// health loops do not generate noise.
func logPhaseTrace(ctx context.Context, path, method string, phases *httpPhases, callErr error) {
	if callErr == nil && phases.connectEstablishMs() < phaseLogSlowThreshold.Milliseconds() {
		return
	}
	entry := logger.FromContext(ctx).
		WithField("path", path).
		WithField("method", method).
		WithField("elapsed_ms", time.Since(phases.start).Milliseconds())
	for k, v := range phases.fields() {
		entry = entry.WithField(k, v)
	}
	if callErr != nil {
		entry = entry.WithError(callErr)
	}
	entry.Warnln("LE client phase trace")
}

var _ Client = (*HTTPClient)(nil)

// Error represents a json-encoded API error.
type Error struct {
	Message string
	Code    int
}

func (e *Error) Error() string {
	return fmt.Sprintf("%d:%s", e.Code, e.Message)
}

func NewHTTPClient(endpoint, serverName, caCertFile, tlsCertFile, tlsKeyFile string) (*HTTPClient, error) {
	tlsCert, err := tls.X509KeyPair([]byte(tlsCertFile), []byte(tlsKeyFile))
	if err != nil {
		return nil, err
	}
	tlsConfig := &tls.Config{
		ServerName:   serverName,
		Certificates: []tls.Certificate{tlsCert},
		MinVersion:   tls.VersionTLS13,
	}

	tlsConfig.RootCAs = x509.NewCertPool()
	tlsConfig.RootCAs.AppendCertsFromPEM([]byte(caCertFile))

	dialer := &net.Dialer{
		Timeout:   dialTimeout,
		KeepAlive: 30 * time.Second, //nolint:mnd
	}
	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           dialer.DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          10,               //nolint:mnd
		IdleConnTimeout:       30 * time.Second, //nolint:mnd
		TLSClientConfig:       tlsConfig,
		TLSHandshakeTimeout:   10 * time.Second, //nolint:mnd
		ExpectContinueTimeout: 1 * time.Second,
		// NOTE: intentionally no ResponseHeaderTimeout. poll_step is a long-poll
		// that legitimately withholds response headers until the step finishes,
		// so a transport-wide header timeout would break it. "Connected but
		// silent" is bounded per-call instead: start_step via its per-attempt
		// context, and any dead connection via the HTTP/2 keepalive ping below.
	}
	// Enable HTTP/2 keepalive pings: if no frame is read for ReadIdleTimeout the
	// transport sends a PING and, absent a PONG within PingTimeout, closes the
	// connection so the request errors out quickly instead of black-holing.
	if h2, h2err := http2.ConfigureTransports(transport); h2err == nil && h2 != nil {
		h2.ReadIdleTimeout = http2ReadIdleTimeout
		h2.PingTimeout = http2PingTimeout
	}
	return &HTTPClient{
		Client: &http.Client{
			Transport: transport,
		},
		Endpoint: endpoint,
	}, nil
}

// HTTPClient provides an http service client.
type HTTPClient struct {
	Client   *http.Client
	Endpoint string
}

// Setup will setup the stage config
func (c *HTTPClient) Setup(ctx context.Context, in *api.SetupRequest) (*api.SetupResponse, error) {
	path := "setup"
	out := new(api.SetupResponse)
	_, err := c.do(ctx, c.Endpoint+path, http.MethodPost, in, out) //nolint:bodyclose
	return out, err
}

// RetrySetup will retry the setup operation
func (c *HTTPClient) RetrySetup(ctx context.Context, in *api.SetupRequest, timeout time.Duration) (*api.SetupResponse, error) {
	startTime := time.Now()
	retryCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var lastErr error
	for i := 0; ; i++ {
		select {
		case <-retryCtx.Done():
			if lastErr != nil {
				return nil, fmt.Errorf("retry setup exhausted after %s: %w", time.Since(startTime), lastErr)
			}
			return nil, retryCtx.Err()
		default:
		}
		// setup is synchronous and variable-duration server-side, so we do NOT
		// impose a sub-budget per-attempt cap (that would abort legitimately
		// slow setups). A dead/black-holed connection is still detected by the
		// HTTP/2 keepalive ping and torn down, letting this loop retry on a
		// fresh connection within the overall budget.
		ret, err := c.Setup(retryCtx, in)
		if err == nil {
			logger.FromContext(ctx).
				WithField("duration", time.Since(startTime)).
				WithField("attempts", i+1).
				Trace("RetrySetup: setup completed")
			return ret, err
		} else if lastErr == nil || (lastErr.Error() != err.Error()) {
			logger.FromContext(ctx).
				WithField("retry_num", i).WithError(err).Traceln("setup failed. Retrying")
			lastErr = err
		}
		time.Sleep(time.Millisecond * 1000) //nolint:mnd
	}
}

// Destroy will clean up the resources created
func (c *HTTPClient) Destroy(ctx context.Context, in *api.DestroyRequest) (*api.DestroyResponse, error) {
	path := "destroy"
	out := new(api.DestroyResponse)
	_, err := c.do(ctx, c.Endpoint+path, http.MethodPost, in, out) //nolint:bodyclose
	return out, err
}

func (c *HTTPClient) StartStep(ctx context.Context, in *api.StartStepRequest) (*api.StartStepResponse, error) {
	path := "start_step"
	out := new(api.StartStepResponse)
	_, err := c.do(ctx, c.Endpoint+path, http.MethodPost, in, out) //nolint:bodyclose
	return out, err
}

func (c *HTTPClient) RetryStartStep(ctx context.Context, in *api.StartStepRequest, timeout time.Duration) (*api.StartStepResponse, error) {
	startTime := time.Now()
	retryCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var lastErr error
	for i := 0; ; i++ {
		select {
		case <-retryCtx.Done():
			if lastErr != nil {
				return nil, fmt.Errorf("retry start step exhausted after %s: %w", time.Since(startTime), lastErr)
			}
			return nil, retryCtx.Err()
		default:
		}
		// Per-attempt timeout so a single stuck call is abandoned and retried
		// (on a fresh connection, which can route around a per-flow black-hole)
		// within the overall budget instead of consuming all of it.
		attemptCtx, cancelAttempt := attemptContext(retryCtx, startStepAttemptTimeout)
		ret, err := c.StartStep(attemptCtx, in)
		cancelAttempt()
		if err == nil {
			logger.FromContext(ctx).
				WithField("duration", time.Since(startTime)).
				WithField("attempts", i+1).
				Trace("RetryStartStep: step started")
			return ret, nil
		} else if lastErr == nil || (lastErr.Error() != err.Error()) {
			logger.FromContext(ctx).
				WithField("retry_num", i).WithError(err).Traceln("start step failed. Retrying")
			lastErr = err
		}
		// Budget-aware backoff: never sleep past the overall deadline, so the
		// remaining budget is spent attempting rather than sleeping.
		select {
		case <-retryCtx.Done():
		case <-time.After(time.Second):
		}
	}
}

func (c *HTTPClient) PollStep(ctx context.Context, in *api.PollStepRequest) (*api.PollStepResponse, error) {
	path := "poll_step"
	out := new(api.PollStepResponse)
	_, err := c.do(ctx, c.Endpoint+path, http.MethodPost, in, out) //nolint:bodyclose
	return out, err
}

// RetryPollStep polls the lite-engine for a step's result. poll_step is a
// server-side LONG-POLL: it blocks until the step completes (bounded only by the
// overall step timeout) before returning a response. We therefore intentionally
// do NOT apply a per-attempt timeout here and the transport has no
// ResponseHeaderTimeout — either would abort in-flight steps. A dead or
// black-holed connection is still detected by the transport's HTTP/2 keepalive
// ping (ReadIdleTimeout/PingTimeout) and torn down, surfacing as an error that
// this loop retries on a fresh connection within the overall budget.
func (c *HTTPClient) RetryPollStep(ctx context.Context, in *api.PollStepRequest, timeout time.Duration) (step *api.PollStepResponse, pollError error) {
	startTime := time.Now()
	retryCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var lastErr error
	for i := 0; ; i++ {
		select {
		case <-retryCtx.Done():
			return step, retryCtx.Err()
		default:
		}

		st := time.Now()
		step, pollError = c.PollStep(retryCtx, in)
		if pollError == nil {
			logger.FromContext(ctx).
				WithField("duration", time.Since(startTime)).
				Trace("RetryPollStep: step completed")
			return step, pollError
		} else if lastErr == nil || (lastErr.Error() != pollError.Error()) {
			logger.FromContext(ctx).
				WithField("retry_num", i).
				WithError(pollError).
				WithField("duration", time.Since(st)).
				Warn("RetryPollStep: polling failed. retrying")
			lastErr = pollError
		}
		time.Sleep(time.Millisecond * 50) //nolint:mnd
	}
}

func (c *HTTPClient) GetStepLogOutput(ctx context.Context, in *api.StreamOutputRequest, w io.Writer) error {
	var r io.Reader

	if in != nil {
		buf := new(bytes.Buffer)
		if err := json.NewEncoder(buf).Encode(in); err != nil {
			logrus.WithError(err).Errorln("failed to encode input")
			return err
		}
		r = buf
	}

	const path = "stream_output"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.Endpoint+path, r)
	if err != nil {
		return err
	}

	res, err := c.Client.Do(req)
	if res != nil {
		defer func() {
			res.Body.Close()
		}()
	}
	if err != nil {
		return err
	}

	if res.StatusCode != http.StatusOK {
		return &Error{Code: res.StatusCode, Message: "failed to stream output"}
	}

	_, err = io.Copy(w, res.Body)

	return err
}

func (c *HTTPClient) Health(ctx context.Context, in *api.HealthRequest) (*api.HealthResponse, error) {
	path := "healthz"
	separator := "?"

	if in.PerformDNSLookup {
		path += separator + "perform_dns_lookup=true"
		separator = "&"
	}
	if in.HealthCheckConnectivityDuration > 0 {
		path += fmt.Sprintf("%sconnectivity_check_duration_seconds=%d", separator, int(in.HealthCheckConnectivityDuration.Seconds()))
	}

	out := new(api.HealthResponse)
	_, err := c.do(ctx, c.Endpoint+path, http.MethodGet, nil, out) //nolint:bodyclose
	return out, err
}

func (c *HTTPClient) RetryHealth(ctx context.Context, in *api.HealthRequest) (*api.HealthResponse, error) {
	startTime := time.Now()
	retryCtx, cancel := context.WithTimeout(ctx, in.Timeout)
	defer cancel()

	var lastErr error
	for i := 0; ; i++ {
		select {
		case <-retryCtx.Done():
			return &api.HealthResponse{}, retryCtx.Err()
		default:
		}
		if ret, err := c.healthCheck(retryCtx, in); err == nil {
			logger.FromContext(ctx).
				WithField("duration", time.Since(startTime)).
				Trace("RetryHealth: health check completed")
			return ret, err
		} else if lastErr == nil || (lastErr.Error() != err.Error()) {
			logger.FromContext(ctx).
				WithField("retry_num", i).WithError(err).Traceln("health check failed. Retrying")
			lastErr = err
		}
		time.Sleep(time.Millisecond * 10) //nolint:mnd
	}
}

func (c *HTTPClient) RetrySuspend(ctx context.Context, request *api.SuspendRequest, timeout time.Duration) (*api.SuspendResponse, error) {
	startTime := time.Now()
	retryCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var lastErr error
	for i := 0; ; i++ {
		select {
		case <-retryCtx.Done():
			return &api.SuspendResponse{}, retryCtx.Err()
		default:
		}
		if ret, err := c.suspend(retryCtx, request); err == nil {
			logger.FromContext(ctx).
				WithField("duration", time.Since(startTime)).
				Trace("RetrySuspend: suspend completed")
			return ret, err
		} else if lastErr == nil || (lastErr.Error() != err.Error()) {
			logger.FromContext(ctx).
				WithField("retry_num", i).WithError(err).Traceln("suspend failed. Retrying")
			lastErr = err
		}
		time.Sleep(time.Millisecond * 10) //nolint:mnd
	}
}

func (c *HTTPClient) healthCheck(ctx context.Context, in *api.HealthRequest) (*api.HealthResponse, error) {
	hctx, cancel := context.WithTimeout(ctx, healthCheckTimeout)
	defer cancel()

	return c.Health(hctx, in)
}

func (c *HTTPClient) suspend(ctx context.Context, in *api.SuspendRequest) (*api.SuspendResponse, error) {
	path := "suspend"
	out := new(api.SuspendResponse)
	_, err := c.do(ctx, c.Endpoint+path, http.MethodPost, in, out) //nolint:bodyclose
	return out, err
}

// do is a helper function that posts a http request with the input encoded and response decoded from json.
func (c *HTTPClient) do(ctx context.Context, path, method string, in, out interface{}) (*http.Response, error) { //nolint:unparam
	var r io.Reader

	if in != nil {
		buf := new(bytes.Buffer)
		if err := json.NewEncoder(buf).Encode(in); err != nil {
			logrus.WithError(err).Errorln("failed to encode input")
			return nil, err
		}
		r = buf
	}

	req, err := http.NewRequestWithContext(ctx, method, path, r)
	if err != nil {
		return nil, err
	}

	// Attach an httptrace so a failed/slow call records the furthest network
	// phase it reached. last_phase distinguishes a SYN black-hole
	// (connect_awaiting_synack) from a server-side stall
	// (wrote_request_awaiting_response) for diagnosing "context deadline
	// exceeded" without VPC flow logs or VM-side logs.
	phases := &httpPhases{start: time.Now()}
	req = req.WithContext(httptrace.WithClientTrace(req.Context(), phases.trace()))

	res, err := c.Client.Do(req)
	logPhaseTrace(ctx, path, method, phases, err)
	if res != nil {
		defer func() {
			// drain the response body so we can reuse
			// this connection.
			if _, cerr := io.Copy(io.Discard, io.LimitReader(res.Body, 4096)); cerr != nil { //nolint:mnd
				logrus.WithError(cerr).Errorln("failed to drain response body")
			}
			res.Body.Close()
		}()
	}
	if err != nil {
		return res, err
	}

	// if the response body return no content we exit
	// immediately. We do not read or unmarshal the response
	// and we do not return an error.
	if res.StatusCode == http.StatusNoContent {
		return res, nil
	}

	// else read the response body into a byte slice.
	body, err := io.ReadAll(res.Body)
	if err != nil {
		return res, err
	}

	if res.StatusCode > 299 { //nolint:mnd
		// if the response body includes an error message
		// we should return the error string.
		if len(body) != 0 {
			out := new(struct {
				Message string `json:"error_msg"`
			})
			if err := json.Unmarshal(body, out); err == nil {
				return res, &Error{Code: res.StatusCode, Message: out.Message}
			}
			return res, &Error{Code: res.StatusCode, Message: string(body)}
		}
		// if the response body is empty we should return
		// the default status code text.
		return res, errors.New(
			http.StatusText(res.StatusCode),
		)
	}
	if out == nil {
		return res, nil
	}
	return res, json.Unmarshal(body, out)
}
