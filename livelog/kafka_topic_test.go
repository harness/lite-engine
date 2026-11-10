// Copyright 2026 Harness Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package livelog

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/harness/lite-engine/logstream/remote"
	"github.com/stretchr/testify/require"
)

// Steps share one log service client but each step's stream must carry the
// topic from its own envs, since a stage can mix metrics and logs health sources.
func TestCustomKafkaTopicIsPerStepStream(t *testing.T) {
	var mu sync.Mutex
	topicsByKey := map[string][]string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			key := r.URL.Query().Get("key")
			mu.Lock()
			topicsByKey[key] = append(topicsByKey[key], r.Header.Get("X-Kafka-Topic"))
			mu.Unlock()
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client := remote.NewHTTPClientWithLogScope(server.URL, "account", "", "", "token", true, false, "", "")
	steps := map[string]map[string]string{
		"metrics-step": {remote.CustomKafkaTopicEnv: "ai-verify-metrics-data"},
		"logs-step":    {remote.CustomKafkaTopicEnv: "ai-verify-logs-data"},
		"plain-step":   {"OTHER": "value"},
	}
	for key, envs := range steps {
		ctx := remote.WithCustomKafkaTopic(context.Background(), remote.CustomKafkaTopicFromEnvs(envs))
		w := New(ctx, client, key, key, nil, false, false, false, false)
		require.NoError(t, w.Open())
		_, err := w.Write([]byte("{\"session_id\":\"s1\"}\n"))
		require.NoError(t, err)
		require.NoError(t, w.Close())
	}

	mu.Lock()
	defer mu.Unlock()
	require.NotEmpty(t, topicsByKey["metrics-step"])
	require.NotEmpty(t, topicsByKey["logs-step"])
	require.NotEmpty(t, topicsByKey["plain-step"])
	for _, topic := range topicsByKey["metrics-step"] {
		require.Equal(t, "ai-verify-metrics-data", topic)
	}
	for _, topic := range topicsByKey["logs-step"] {
		require.Equal(t, "ai-verify-logs-data", topic)
	}
	for _, topic := range topicsByKey["plain-step"] {
		require.Equal(t, "", topic)
	}
}
