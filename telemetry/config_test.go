package telemetry

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfigValidate(t *testing.T) {
	tests := []struct {
		name    string
		cfg     Config
		wantErr string // substring; empty means valid
	}{
		{name: "disabled config is always valid, even if empty", cfg: Config{}},
		{name: "http endpoint", cfg: Config{Enabled: true, Endpoint: "http://172.22.64.10:4318"}},
		{name: "https endpoint", cfg: Config{Enabled: true, Endpoint: "https://otlp.example.com"}},
		{name: "enabled without endpoint", cfg: Config{Enabled: true}, wantErr: "endpoint"},
		{name: "endpoint without scheme", cfg: Config{Enabled: true, Endpoint: "172.22.64.10:4318"}, wantErr: "endpoint"},
		{name: "unsupported scheme", cfg: Config{Enabled: true, Endpoint: "grpc://host:4317"}, wantErr: "scheme"},
		{name: "endpoint without host", cfg: Config{Enabled: true, Endpoint: "http://"}, wantErr: "host"},
		{
			name:    "label name that is not a valid prometheus label",
			cfg:     Config{Enabled: true, Endpoint: testEndpoint, MetricLabels: map[string]string{"bad-name": "x"}},
			wantErr: "label",
		},
		{
			name:    "label that would shadow a built-in dimension",
			cfg:     Config{Enabled: true, Endpoint: testEndpoint, MetricLabels: map[string]string{wireOutcomeAttr: "x"}},
			wantErr: "reserved",
		},
		{
			name:    "vm_id cannot be smuggled in through labels",
			cfg:     Config{Enabled: true, Endpoint: testEndpoint, MetricLabels: map[string]string{"vm_id": "x"}},
			wantErr: "reserved",
		},
		{
			name: "fleet-level labels are accepted",
			cfg:  Config{Enabled: true, Endpoint: testEndpoint, MetricLabels: map[string]string{testPoolLabel: testPool, "region": "us-west1"}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.validate()
			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestConfigWithDefaultsFillsOnlyUnsetTunables(t *testing.T) {
	got := Config{}.withDefaults()

	assert.Equal(t, 30*time.Second, got.Interval)
	assert.Equal(t, 5*time.Second, got.ExportTimeout)

	custom := Config{Interval: time.Second, ExportTimeout: 2 * time.Second}.withDefaults()
	assert.Equal(t, time.Second, custom.Interval)
	assert.Equal(t, 2*time.Second, custom.ExportTimeout)
}

func TestResolveVMID(t *testing.T) {
	tests := []struct {
		name     string
		hostname func() (string, error)
		want     string
	}{
		{name: "uses the hostname", hostname: func() (string, error) { return "vm-abc", nil }, want: "vm-abc"},
		{name: "hostname lookup failure falls back", hostname: func() (string, error) { return "", errors.New("no") }, want: "unknown"},
		{name: "empty hostname falls back", hostname: func() (string, error) { return "", nil }, want: "unknown"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, resolveVMID(tt.hostname))
		})
	}
}
