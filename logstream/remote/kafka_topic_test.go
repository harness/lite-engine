// Copyright 2026 Harness Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package remote

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/harness/lite-engine/logstream"
	"github.com/stretchr/testify/require"
)

func TestCustomKafkaTopicFromEnvs(t *testing.T) {
	tests := []struct {
		name  string
		envs  map[string]string
		topic string
	}{
		{name: "nil envs", envs: nil, topic: ""},
		{name: "absent", envs: map[string]string{"OTHER": "x"}, topic: ""},
		{name: "empty", envs: map[string]string{CustomKafkaTopicEnv: ""}, topic: ""},
		{name: "metrics topic", envs: map[string]string{CustomKafkaTopicEnv: "ai-verify-metrics-data"}, topic: "ai-verify-metrics-data"},
		{name: "logs topic", envs: map[string]string{CustomKafkaTopicEnv: "ai-verify-logs-data"}, topic: "ai-verify-logs-data"},
		{name: "surrounding whitespace", envs: map[string]string{CustomKafkaTopicEnv: "  ai-verify-logs-data\n"}, topic: "ai-verify-logs-data"},
		{name: "dots and underscores", envs: map[string]string{CustomKafkaTopicEnv: "ai-verify-a.b_c"}, topic: "ai-verify-a.b_c"},
		{name: "max length", envs: map[string]string{CustomKafkaTopicEnv: "ai-verify-" + strings.Repeat("a", 239)}, topic: "ai-verify-" + strings.Repeat("a", 239)},
		{name: "too long", envs: map[string]string{CustomKafkaTopicEnv: "ai-verify-" + strings.Repeat("a", 240)}, topic: ""},
		{name: "prefix only", envs: map[string]string{CustomKafkaTopicEnv: "ai-verify-"}, topic: ""},
		{name: "not allowed prefix", envs: map[string]string{CustomKafkaTopicEnv: "harness-logs"}, topic: ""},
		{name: "ends with dot", envs: map[string]string{CustomKafkaTopicEnv: "ai-verify-logs."}, topic: ""},
		{name: "invalid character", envs: map[string]string{CustomKafkaTopicEnv: "ai-verify-logs/data"}, topic: ""},
		{name: "unresolved expression", envs: map[string]string{CustomKafkaTopicEnv: "<+input>"}, topic: ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require.Equal(t, test.topic, CustomKafkaTopicFromEnvs(test.envs))
		})
	}
}

func TestCustomKafkaTopicContext(t *testing.T) {
	ctx := context.Background()
	require.Equal(t, "", CustomKafkaTopicFromContext(ctx))
	require.Equal(t, ctx, WithCustomKafkaTopic(ctx, ""))

	ctx = WithCustomKafkaTopic(ctx, "ai-verify-logs-data")
	require.Equal(t, "ai-verify-logs-data", CustomKafkaTopicFromContext(ctx))
}

type headerRecorder struct {
	mu      sync.Mutex
	headers map[string][]string // method -> X-Kafka-Topic values seen
}

func newHeaderRecordingServer(t *testing.T) (*httptest.Server, *headerRecorder) {
	t.Helper()
	rec := &headerRecorder{headers: map[string][]string{}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.mu.Lock()
		rec.headers[r.Method] = append(rec.headers[r.Method], r.Header.Get(customKafkaTopicHeader))
		rec.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(server.Close)
	return server, rec
}

func (r *headerRecorder) get(method string) []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.headers[method]
}

var indirectUploadClientFactories = []struct {
	name string
	new  func(endpoint string) logstream.Client
}{
	{
		name: "standard",
		new: func(endpoint string) logstream.Client {
			return NewHTTPClientWithLogScope(endpoint, "account", "", "", "token", true, false, "", "")
		},
	},
	{
		name: "optimized",
		new: func(endpoint string) logstream.Client {
			return NewOptimizedHTTPClientWithLogScope(endpoint, "account", "", "", "token", true, false, "", "")
		},
	},
}

func TestCustomKafkaTopicHeaderOnlyOnWrite(t *testing.T) {
	for _, factory := range indirectUploadClientFactories {
		t.Run(factory.name, func(t *testing.T) {
			server, rec := newHeaderRecordingServer(t)
			client := factory.new(server.URL)
			ctx := WithCustomKafkaTopic(context.Background(), "ai-verify-metrics-data")
			lines := []*logstream.Line{{Message: "hello"}}

			require.NoError(t, client.Open(ctx, "log-key"))
			require.NoError(t, client.Write(ctx, "log-key", lines))
			require.NoError(t, client.Upload(ctx, "log-key", lines))
			require.NoError(t, client.Close(ctx, "log-key", false))

			require.Equal(t, []string{"ai-verify-metrics-data"}, rec.get(http.MethodPut))
			// Open and the indirect upload are both POSTs.
			require.Equal(t, []string{"", ""}, rec.get(http.MethodPost))
			require.Equal(t, []string{""}, rec.get(http.MethodDelete))
		})
	}
}

func TestNoCustomKafkaTopicHeaderWithoutTopic(t *testing.T) {
	for _, factory := range indirectUploadClientFactories {
		t.Run(factory.name, func(t *testing.T) {
			server, rec := newHeaderRecordingServer(t)
			client := factory.new(server.URL)

			require.NoError(t, client.Write(context.Background(), "log-key", []*logstream.Line{{Message: "hello"}}))
			require.Equal(t, []string{""}, rec.get(http.MethodPut))
		})
	}
}
