package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/harness/lite-engine/api"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestClient(url string) *HTTPClient {
	return &HTTPClient{
		Client:   &http.Client{},
		Endpoint: url + "/",
	}
}

func TestDo_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}))
	defer srv.Close()

	client := newTestClient(srv.URL)
	out := &api.HealthResponse{}
	_, err := client.do(context.Background(), srv.URL+"/healthz", http.MethodGet, nil, out)
	assert.NoError(t, err)
}

func TestDo_NoContent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	client := newTestClient(srv.URL)
	_, err := client.do(context.Background(), srv.URL+"/destroy", http.MethodPost, nil, nil)
	assert.NoError(t, err)
}

func TestDo_ServerError_WithBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error_msg": "something broke"})
	}))
	defer srv.Close()

	client := newTestClient(srv.URL)
	_, err := client.do(context.Background(), srv.URL+"/setup", http.MethodPost, &api.SetupRequest{}, nil)
	require.Error(t, err)

	var apiErr *Error
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, http.StatusInternalServerError, apiErr.Code)
	assert.Equal(t, "something broke", apiErr.Message)
}

func TestDo_ServerError_EmptyBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()

	client := newTestClient(srv.URL)
	_, err := client.do(context.Background(), srv.URL+"/setup", http.MethodPost, nil, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Bad Gateway")
}

func TestDo_ContextDeadlineExceeded_LogsTrace(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	hook := &captureHook{}
	logrus.StandardLogger().AddHook(hook)
	logrus.SetLevel(logrus.WarnLevel)
	defer logrus.StandardLogger().ReplaceHooks(make(logrus.LevelHooks))

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	client := newTestClient(srv.URL)
	_, err := client.do(ctx, srv.URL+"/poll_step", http.MethodPost, &api.PollStepRequest{ID: "step1"}, nil)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "context deadline exceeded")

	require.NotEmpty(t, hook.entries, "expected a warn log from logHTTPFailure")
	entry := hook.entries[0]
	assert.Equal(t, logrus.WarnLevel, entry.Level)
	assert.Equal(t, "lite-engine request failed", entry.Message)
	assert.Contains(t, entry.Data, "method")
	assert.Contains(t, entry.Data, "path")
	assert.Contains(t, entry.Data, "conn_reused")
}

func TestDo_ConnectionRefused_LogsTrace(t *testing.T) {
	hook := &captureHook{}
	logrus.StandardLogger().AddHook(hook)
	logrus.SetLevel(logrus.WarnLevel)
	defer logrus.StandardLogger().ReplaceHooks(make(logrus.LevelHooks))

	client := &HTTPClient{
		Client:   &http.Client{Timeout: 2 * time.Second},
		Endpoint: "http://127.0.0.1:19999/",
	}

	_, err := client.do(context.Background(), "http://127.0.0.1:19999/healthz", http.MethodGet, nil, nil)
	require.Error(t, err)

	require.NotEmpty(t, hook.entries)
	entry := hook.entries[0]
	assert.Equal(t, "lite-engine request failed", entry.Message)
	assert.Equal(t, "http://127.0.0.1:19999/healthz", entry.Data["path"])
}

func TestRetryHealth_SucceedsAfterRetries(t *testing.T) {
	attempts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(api.HealthResponse{})
	}))
	defer srv.Close()

	client := newTestClient(srv.URL)
	_, err := client.RetryHealth(context.Background(), &api.HealthRequest{
		Timeout: 5 * time.Second,
	})
	assert.NoError(t, err)
	assert.GreaterOrEqual(t, attempts, 3)
}

func TestRetryHealth_TimesOut(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	client := newTestClient(srv.URL)
	_, err := client.RetryHealth(context.Background(), &api.HealthRequest{
		Timeout: 100 * time.Millisecond,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "context deadline exceeded")
}

func TestRetryPollStep_SucceedsAfterRetries(t *testing.T) {
	attempts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(api.PollStepResponse{})
	}))
	defer srv.Close()

	client := newTestClient(srv.URL)
	_, err := client.RetryPollStep(context.Background(), &api.PollStepRequest{ID: "s1"}, 5*time.Second)
	assert.NoError(t, err)
	assert.GreaterOrEqual(t, attempts, 3)
}

func TestTraceCollector_CapturesDNSAndConnect(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{})
	}))
	defer srv.Close()

	// Use localhost hostname to trigger DNS
	client := &HTTPClient{
		Client:   &http.Client{},
		Endpoint: srv.URL + "/",
	}
	out := make(map[string]string)
	_, err := client.do(context.Background(), srv.URL+"/healthz", http.MethodGet, nil, &out)
	assert.NoError(t, err)
}

// captureHook captures logrus entries for assertions
type captureHook struct {
	mu      sync.Mutex
	entries []*logrus.Entry
}

func (h *captureHook) Levels() []logrus.Level {
	return logrus.AllLevels
}

func (h *captureHook) Fire(entry *logrus.Entry) error {
	h.mu.Lock()
	h.entries = append(h.entries, entry)
	h.mu.Unlock()
	return nil
}

// TestHTTPClient_ConcurrentDo drives many goroutines through the same
// HTTPClient.do() against a single httptest server. Goes through the shared
// http.Transport / connection pool. Run with `go test -race -count=5`.
func TestHTTPClient_ConcurrentDo(t *testing.T) {
	var hits int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&hits, 1)
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}))
	defer srv.Close()

	client := newTestClient(srv.URL)

	const (
		goroutines     = 32
		requestsPerGor = 20
	)

	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < requestsPerGor; j++ {
				out := map[string]string{}
				_, err := client.do(context.Background(), srv.URL+"/healthz", http.MethodGet, nil, &out)
				if err != nil {
					t.Errorf("do: %v", err)
					return
				}
			}
		}()
	}
	wg.Wait()

	if got := atomic.LoadInt64(&hits); got != int64(goroutines*requestsPerGor) {
		t.Fatalf("server hits = %d, want %d", got, goroutines*requestsPerGor)
	}
}

// TestHTTPClient_ConcurrentMixedEndpoints exercises every public method that
// wraps do() in parallel from many goroutines. Catches races on any shared
// state (transport pools, package-level vars like healthCheckTimeout, etc.).
func TestHTTPClient_ConcurrentMixedEndpoints(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		switch r.URL.Path {
		case "/healthz":
			_ = json.NewEncoder(w).Encode(api.HealthResponse{})
		case "/setup":
			_ = json.NewEncoder(w).Encode(api.SetupResponse{})
		case "/start_step":
			_ = json.NewEncoder(w).Encode(api.StartStepResponse{})
		case "/poll_step":
			_ = json.NewEncoder(w).Encode(api.PollStepResponse{})
		case "/destroy":
			_ = json.NewEncoder(w).Encode(api.DestroyResponse{})
		case "/suspend":
			_ = json.NewEncoder(w).Encode(api.SuspendResponse{})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	client := newTestClient(srv.URL)
	ctx := context.Background()

	ops := []func() error{
		func() error {
			_, err := client.Health(ctx, &api.HealthRequest{})
			return err
		},
		func() error {
			_, err := client.Setup(ctx, &api.SetupRequest{})
			return err
		},
		func() error {
			_, err := client.StartStep(ctx, &api.StartStepRequest{})
			return err
		},
		func() error {
			_, err := client.PollStep(ctx, &api.PollStepRequest{ID: "s"})
			return err
		},
		func() error {
			_, err := client.Destroy(ctx, &api.DestroyRequest{})
			return err
		},
		func() error {
			_, err := client.suspend(ctx, &api.SuspendRequest{})
			return err
		},
	}

	const perOp = 25
	var wg sync.WaitGroup
	for i, op := range ops {
		for j := 0; j < perOp; j++ {
			wg.Add(1)
			go func(i, j int, op func() error) {
				defer wg.Done()
				if err := op(); err != nil {
					t.Errorf("op[%d] iter %d: %v", i, j, err)
				}
			}(i, j, op)
		}
	}
	wg.Wait()
}

// TestHTTPClient_ConcurrentRetry races the retry loops against each other.
// Each retry method holds its own retryCtx but they share c.Client and
// (transitively) the transport's connection pool.
func TestHTTPClient_ConcurrentRetry(t *testing.T) {
	var attempts int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt64(&attempts, 1)
		// fail the first ~20% of requests so the retry path actually runs
		if n%5 == 0 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
		switch r.URL.Path {
		case "/healthz":
			_ = json.NewEncoder(w).Encode(api.HealthResponse{})
		case "/poll_step":
			_ = json.NewEncoder(w).Encode(api.PollStepResponse{})
		case "/start_step":
			_ = json.NewEncoder(w).Encode(api.StartStepResponse{})
		case "/setup":
			_ = json.NewEncoder(w).Encode(api.SetupResponse{})
		case "/suspend":
			_ = json.NewEncoder(w).Encode(api.SuspendResponse{})
		}
	}))
	defer srv.Close()

	client := newTestClient(srv.URL)

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = client.RetryHealth(context.Background(), &api.HealthRequest{Timeout: 5 * time.Second})
		}()
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = client.RetryPollStep(context.Background(), &api.PollStepRequest{ID: "s"}, 5*time.Second)
		}()
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = client.RetryStartStep(context.Background(), &api.StartStepRequest{}, 15*time.Second)
		}()
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = client.RetrySetup(context.Background(), &api.SetupRequest{}, 5*time.Second)
		}()
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = client.RetrySuspend(context.Background(), &api.SuspendRequest{}, 5*time.Second)
		}()
	}
	wg.Wait()
}

// TestHTTPClient_ConcurrentRequestCancellation races request cancellation
// against in-flight requests on the same client. This is the closest
// reproduction of the HTTP/2 stall scenario fixed in d10dd882: many requests
// in flight, some get canceled mid-flight, the connection pool should not
// race on takedown of the canceled stream.
func TestHTTPClient_ConcurrentRequestCancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// slow handler so most requests are mid-flight when canceled
		select {
		case <-r.Context().Done():
			return
		case <-time.After(100 * time.Millisecond):
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{"ok": "true"})
	}))
	defer srv.Close()

	client := newTestClient(srv.URL)

	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			// Mix of canceled and full-duration requests
			timeout := 200 * time.Millisecond
			if i%2 == 0 {
				timeout = 20 * time.Millisecond // these will be canceled
			}
			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			defer cancel()
			out := map[string]string{}
			_, _ = client.do(ctx, srv.URL+"/healthz", http.MethodGet, nil, &out)
		}(i)
	}
	wg.Wait()
}
