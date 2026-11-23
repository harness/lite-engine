// Copyright 2026 Harness Inc. All rights reserved.
// Use of this source code is governed by the PolyForm Free Trial 1.0.0 license
// that can be found in the licenses directory at the root of this repository, also available at
// https://polyformproject.org/wp-content/uploads/2020/05/PolyForm-Free-Trial-1.0.0.txt.

package docker

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/harness/lite-engine/egress/redirect"
	"github.com/harness/lite-engine/engine/spec"
)

// egressRecorder stands in for the real egress actions and records which of
// them configureEgress ran, in order.
type egressRecorder struct {
	calls   []string
	flagOn  bool
	failAt  string
	failErr error
	proxy   redirect.Proxy
}

func (r *egressRecorder) step(name string) error {
	r.calls = append(r.calls, name)
	if r.failAt == name {
		return r.failErr
	}
	return nil
}

func (r *egressRecorder) steps() *egressSteps {
	return &egressSteps{
		applies: func(proxyURL string, privateConnectivity bool) bool {
			return r.flagOn && !privateConnectivity && strings.TrimSpace(proxyURL) != ""
		},
		clearProxy: func(context.Context, string) error { return r.step("clear-private-proxy") },
		install: func(_ context.Context, p redirect.Proxy) error {
			r.proxy = p
			return r.step("install-redirect")
		},
		remove:     func(context.Context, string) error { return r.step("remove-redirect") },
		applyProxy: func(context.Context, *spec.EgressProxyConfig, string) error { return r.step("apply-docker-proxy") },
		daemonProxy: func(context.Context, *spec.PipelineConfig) {
			_ = r.step("set-daemon-proxy")
		},
	}
}

func egressConfig(goos string, private bool, policy *spec.EgressProxyConfig, envs map[string]string) *spec.PipelineConfig {
	return &spec.PipelineConfig{
		Platform:            spec.Platform{OS: goos},
		PrivateConnectivity: private,
		EgressProxy:         policy,
		Envs:                envs,
	}
}

func TestConfigureEgressPicksTheRightAction(t *testing.T) {
	policy := &spec.EgressProxyConfig{ProxyURL: "http://proxy.example:3128", Username: "acct|org", Password: "token", NoProxy: "localhost"}
	emptyPolicy := &spec.EgressProxyConfig{}
	legacyEnv := map[string]string{harnessHTTPSProxy: "http://legacy.example:3128"}

	tests := []struct {
		name   string
		flagOn bool
		cfg    *spec.PipelineConfig
		want   []string
	}{
		{
			name: "private connectivity clears the proxy and removes a redirect left by an earlier stage",
			cfg:  egressConfig("linux", true, nil, nil), flagOn: true,
			want: []string{"clear-private-proxy", "remove-redirect"},
		},
		{
			name: "private connectivity never installs the redirect, even with a proxy policy",
			cfg:  egressConfig("linux", true, policy, nil), flagOn: true,
			want: []string{"clear-private-proxy", "remove-redirect"},
		},
		{
			name: "sidecar flag on, Linux, proxy policy: install the redirect",
			cfg:  egressConfig("linux", false, policy, nil), flagOn: true,
			want: []string{"install-redirect"},
		},
		{
			name: "sidecar flag off: the explicit Docker proxy",
			cfg:  egressConfig("linux", false, policy, nil),
			want: []string{"apply-docker-proxy"},
		},
		{
			name: "sidecar flag on but not Linux: the explicit Docker proxy",
			cfg:  egressConfig("windows", false, policy, nil), flagOn: true,
			want: []string{"apply-docker-proxy"},
		},
		{
			name: "policy without a proxy URL falls through to cleanup",
			cfg:  egressConfig("linux", false, emptyPolicy, nil), flagOn: true,
			want: []string{"remove-redirect"},
		},
		{
			name: "no policy, legacy proxy variable: set the daemon proxy, then clean up",
			cfg:  egressConfig("linux", false, nil, legacyEnv),
			want: []string{"set-daemon-proxy", "remove-redirect"},
		},
		{
			name: "no policy, no proxy: only clean up a redirect left behind",
			cfg:  egressConfig("linux", false, nil, nil), flagOn: true,
			want: []string{"remove-redirect"},
		},
		{
			name: "macOS with no proxy still goes through cleanup, which does nothing there",
			cfg:  egressConfig("darwin", false, nil, nil),
			want: []string{"remove-redirect"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := &egressRecorder{flagOn: tt.flagOn}
			d := &Docker{egress: rec.steps()}
			if err := d.configureEgress(context.Background(), tt.cfg); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(rec.calls, tt.want) {
				t.Fatalf("ran %v, want %v", rec.calls, tt.want)
			}
		})
	}
}

func TestConfigureEgressHandsThePolicyToTheRedirect(t *testing.T) {
	rec := &egressRecorder{flagOn: true}
	d := &Docker{egress: rec.steps()}
	policy := &spec.EgressProxyConfig{ProxyURL: "http://proxy.example:3128", Username: "acct|org", Password: "token", NoProxy: "localhost,10.0.0.0/8"}
	if err := d.configureEgress(context.Background(), egressConfig("linux", false, policy, nil)); err != nil {
		t.Fatal(err)
	}
	want := redirect.Proxy{URL: policy.ProxyURL, Username: policy.Username, Password: policy.Password, NoProxy: policy.NoProxy}
	if rec.proxy != want {
		t.Fatalf("redirect got %+v, want %+v", rec.proxy, want)
	}
}

func TestConfigureEgressWrapsAndStopsOnAFailedAction(t *testing.T) {
	policy := &spec.EgressProxyConfig{ProxyURL: "http://proxy.example:3128"}
	boom := errors.New("boom")
	tests := []struct {
		name      string
		flagOn    bool
		cfg       *spec.PipelineConfig
		failAt    string
		wantText  string
		wantCalls []string
	}{
		{
			name: "install", flagOn: true, cfg: egressConfig("linux", false, policy, nil),
			failAt: "install-redirect", wantText: "failed to install egress sidecar",
			wantCalls: []string{"install-redirect"},
		},
		{
			name: "docker proxy", cfg: egressConfig("linux", false, policy, nil),
			failAt: "apply-docker-proxy", wantText: "failed to configure docker proxy for egress",
			wantCalls: []string{"apply-docker-proxy"},
		},
		{
			name: "clearing the private proxy", cfg: egressConfig("linux", true, nil, nil),
			failAt: "clear-private-proxy", wantText: "failed to clear inherited proxy before private connectivity setup",
			wantCalls: []string{"clear-private-proxy"},
		},
		{
			name: "removing a left-behind redirect", cfg: egressConfig("linux", false, nil, nil),
			failAt: "remove-redirect", wantText: boom.Error(),
			wantCalls: []string{"remove-redirect"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := &egressRecorder{flagOn: tt.flagOn, failAt: tt.failAt, failErr: boom}
			d := &Docker{egress: rec.steps()}
			err := d.configureEgress(context.Background(), tt.cfg)
			if !errors.Is(err, boom) || !strings.Contains(err.Error(), tt.wantText) {
				t.Fatalf("err = %v, want it to wrap %q and %q", err, boom, tt.wantText)
			}
			if !reflect.DeepEqual(rec.calls, tt.wantCalls) {
				t.Fatalf("ran %v, want %v", rec.calls, tt.wantCalls)
			}
		})
	}
}

// Without the test seam, Setup uses the real actions. On a host that is not
// Linux, or with CI_EGRESS_SIDECAR unset, that must change nothing and not fail.
func TestConfigureEgressWithTheRealActionsDoesNothingWithoutAProxy(t *testing.T) {
	t.Setenv("CI_EGRESS_SIDECAR", "")
	d := &Docker{}
	if err := d.configureEgress(context.Background(), egressConfig("darwin", false, nil, nil)); err != nil {
		t.Fatal(err)
	}
}
