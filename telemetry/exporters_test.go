package telemetry

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSignalURL(t *testing.T) {
	tests := []struct {
		name, base, signal, want string
	}{
		{"bare host", "http://172.22.64.10:4318", "v1/metrics", "http://172.22.64.10:4318/v1/metrics"},
		{"trailing slash", "http://gw:4318/", "v1/logs", "http://gw:4318/v1/logs"},
		{"gateway path prefix is preserved", "https://gw.example.com/otel", "v1/metrics", "https://gw.example.com/otel/v1/metrics"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, signalURL(tt.base, tt.signal))
		})
	}
}
