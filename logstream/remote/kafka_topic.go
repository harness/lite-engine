// Copyright 2026 Harness Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package remote

import (
	"context"
	"net/http"
	"regexp"
	"strings"

	"github.com/sirupsen/logrus"
)

const (
	// CustomKafkaTopicEnv is the step env var naming the extra Kafka topic
	// that log service should also publish the step's log lines to.
	CustomKafkaTopicEnv = "PLUGIN_HARNESS_LOG_SERVICE_CUSTOM_KAFKA_TOPIC"

	customKafkaTopicHeader = "X-Kafka-Topic"
)

// Mirrors log service validation: Kafka topic naming rules (max 249 chars,
// cannot end with '.' or '-') restricted to the ai-verify- prefix. Log service
// rejects the whole write for a topic it does not accept, so invalid values
// must never be sent.
var customKafkaTopicRegex = regexp.MustCompile(`^ai-verify-[a-zA-Z0-9._-]{0,238}[a-zA-Z0-9]$`)

type customKafkaTopicKey struct{}

// WithCustomKafkaTopic returns a context carrying the custom Kafka topic.
// An empty topic returns ctx unchanged.
func WithCustomKafkaTopic(ctx context.Context, topic string) context.Context {
	if topic == "" {
		return ctx
	}
	return context.WithValue(ctx, customKafkaTopicKey{}, topic)
}

// CustomKafkaTopicFromContext returns the custom Kafka topic set via
// WithCustomKafkaTopic, or "" if none is set.
func CustomKafkaTopicFromContext(ctx context.Context) string {
	topic, _ := ctx.Value(customKafkaTopicKey{}).(string)
	return topic
}

// CustomKafkaTopicFromEnvs returns the validated custom Kafka topic from the
// step envs, or "" if it is absent or invalid.
func CustomKafkaTopicFromEnvs(envs map[string]string) string {
	topic := strings.TrimSpace(envs[CustomKafkaTopicEnv])
	if topic == "" {
		return ""
	}
	if !customKafkaTopicRegex.MatchString(topic) {
		logrus.WithField("topic", topic).
			Warnln("ignoring invalid custom kafka topic for log stream")
		return ""
	}
	return topic
}

// customKafkaTopicOpt sets the X-Kafka-Topic header from ctx. It is applied
// to stream writes only; open, close and upload requests never carry it.
func customKafkaTopicOpt(ctx context.Context) func(*http.Request) {
	return func(req *http.Request) {
		if topic := CustomKafkaTopicFromContext(ctx); topic != "" {
			req.Header.Set(customKafkaTopicHeader, topic)
		}
	}
}
