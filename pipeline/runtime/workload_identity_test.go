// Copyright 2026 Harness Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package runtime

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/harness/lite-engine/api"
	"github.com/harness/lite-engine/engine/spec"
)

func TestRegisterWorkloadIdentities_InjectsHandleAndStripsTokens(t *testing.T) {
	r := &api.StartStepRequest{
		ID: "step1",
		WorkloadIdentities: []api.WorkloadIdentity{
			{Name: "AWS_ID_TOKEN", WorkloadToken: "wtok", Audience: "https://sts.amazonaws.com", TokenMode: "STANDARD"},
		},
		WITokenGenerateURL: "https://harnessid/token/generate",
	}

	handle := registerWorkloadIdentities(r)
	if handle == "" {
		t.Fatal("expected a non-empty handle")
	}
	// handle injected into the step env
	if r.Envs[wiHandleEnv] != handle {
		t.Errorf("HARNESS_WI_HANDLE not injected: got %q", r.Envs[wiHandleEnv])
	}
	// on Linux/Mac the mint URL is the Unix socket (no network port); same path host + container
	if got, want := r.Envs[wiMintURLEnv], "unix://"+filepath.Join(spec.WISocketDir, spec.WISocketName); got != want {
		t.Errorf("HARNESS_WI_MINT_URL = %q, want %q", got, want)
	}
	// workload tokens are stripped from the request (never reach the container)
	if r.WorkloadIdentities != nil {
		t.Error("expected WorkloadIdentities to be stripped after registration")
	}
	// the Unix socket needs no host-gateway extra host
	for _, h := range r.ExtraHosts {
		if h == hostGatewayExtraHost {
			t.Errorf("did not expect host-gateway extra host on the unix-socket path: %v", r.ExtraHosts)
		}
	}
	// the identity is retrievable from the store under the handle
	if wi, tokenURL, ok := wiStore.get(handle, "AWS_ID_TOKEN"); !ok {
		t.Error("identity not stored")
	} else if wi.WorkloadToken != "wtok" || tokenURL != "https://harnessid/token/generate" { //nolint:gosec // test literal URL, not a credential
		t.Errorf("stored identity mismatch: wi=%+v url=%s", wi, tokenURL)
	}
}

func TestRegisterWorkloadIdentities_NoIdentities_NoOp(t *testing.T) {
	r := &api.StartStepRequest{ID: "step2", Envs: map[string]string{"FOO": "bar"}}
	if handle := registerWorkloadIdentities(r); handle != "" {
		t.Errorf("expected empty handle for no identities, got %q", handle)
	}
	if _, ok := r.Envs[wiHandleEnv]; ok {
		t.Error("did not expect HARNESS_WI_HANDLE to be injected")
	}
}

func TestMintWorkloadToken_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if got := req.Header.Get(workloadTokenHeader); got != "wtok" {
			t.Errorf("expected Workload-Token header wtok, got %q", got)
		}
		var body oidcTokenRequest
		_ = json.NewDecoder(req.Body).Decode(&body)
		if body.Audience != "https://sts.amazonaws.com" {
			t.Errorf("unexpected audience %q", body.Audience)
		}
		_ = json.NewEncoder(w).Encode(oidcTokenResponse{Token: "oidc.jwt.token", ExpiresIn: 3600})
	}))
	defer srv.Close()

	handle := "handle-success"
	wiStore.put(handle, []api.WorkloadIdentity{
		{Name: "AWS_ID_TOKEN", WorkloadToken: "wtok", Audience: "https://sts.amazonaws.com", TokenMode: "STANDARD"},
	}, srv.URL)
	defer wiStore.delete(handle)

	resp := MintWorkloadToken(context.Background(), api.MintWorkloadTokenRequest{Handle: handle, Name: "AWS_ID_TOKEN"})
	if resp.Error != "" {
		t.Fatalf("unexpected error: %s", resp.Error)
	}
	if resp.OidcToken != "oidc.jwt.token" {
		t.Errorf("unexpected token %q", resp.OidcToken)
	}
	if resp.ExpiresAtUnix == 0 {
		t.Error("expected a non-zero expiry")
	}
}

func TestMintWorkloadToken_UnknownHandle(t *testing.T) {
	resp := MintWorkloadToken(context.Background(), api.MintWorkloadTokenRequest{Handle: "nope", Name: "X"})
	if resp.Error != ErrUnknownWorkloadIdentity {
		t.Errorf("expected %q for unknown handle, got %q", ErrUnknownWorkloadIdentity, resp.Error)
	}
	if resp.OidcToken != "" {
		t.Error("expected no token for unknown handle")
	}
}

// A handle evicted on step completion (wiStore.delete) can no longer mint - the revocation guarantee.
func TestMintWorkloadToken_EvictedHandleRejected(t *testing.T) {
	handle := "handle-evict"
	wiStore.put(handle, []api.WorkloadIdentity{
		{Name: "ID", WorkloadToken: "wtok", Audience: "aud"},
	}, "https://harnessid/token/generate")

	wiStore.delete(handle) // simulate step completion evicting the handle

	resp := MintWorkloadToken(context.Background(), api.MintWorkloadTokenRequest{Handle: handle, Name: "ID"})
	if resp.Error != ErrUnknownWorkloadIdentity {
		t.Errorf("evicted handle should be rejected with %q, got %q", ErrUnknownWorkloadIdentity, resp.Error)
	}
	if resp.OidcToken != "" {
		t.Error("evicted handle must not mint")
	}
}

// On an upstream (HarnessID) failure the step must get a generic error, never HarnessID's raw body.
func TestMintWorkloadToken_UpstreamErrorReturnsGenericWithoutLeak(t *testing.T) {
	const secret = "INTERNAL-HARNESSID-DIAGNOSTIC-DO-NOT-LEAK"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"` + secret + `"}`))
	}))
	defer srv.Close()

	handle := "handle-upstream-err"
	wiStore.put(handle, []api.WorkloadIdentity{{Name: "ID", WorkloadToken: "wtok", Audience: "aud"}}, srv.URL)
	defer wiStore.delete(handle)

	resp := MintWorkloadToken(context.Background(), api.MintWorkloadTokenRequest{Handle: handle, Name: "ID"})
	if resp.Error != "failed to mint workload identity token" {
		t.Errorf("expected generic error, got %q", resp.Error)
	}
	if strings.Contains(resp.Error, secret) {
		t.Errorf("mint error leaked HarnessID internal detail to the step: %q", resp.Error)
	}
	if resp.OidcToken != "" {
		t.Error("expected no token on failure")
	}
}

// ClearWorkloadIdentities (stage teardown) drops every entry, including a detached step's handle that
// was never evicted on completion - so nothing is mintable after the stage is destroyed.
func TestClearWorkloadIdentities_DropsAllHandles(t *testing.T) {
	wiStore.put("h1", []api.WorkloadIdentity{{Name: "ID", WorkloadToken: "wtok", Audience: "aud"}}, "https://harnessid/token/generate")
	wiStore.put("h2", []api.WorkloadIdentity{{Name: "ID", WorkloadToken: "wtok", Audience: "aud"}}, "https://harnessid/token/generate")

	ClearWorkloadIdentities()

	for _, h := range []string{"h1", "h2"} {
		resp := MintWorkloadToken(context.Background(), api.MintWorkloadTokenRequest{Handle: h, Name: "ID"})
		if resp.Error != ErrUnknownWorkloadIdentity {
			t.Errorf("handle %q should be gone after clear, got error %q", h, resp.Error)
		}
	}
}

func TestMintBindAddress(t *testing.T) {
	t.Setenv(wiMintBindEnv, "")
	if got, want := MintBindAddress(), ":"+defaultMintPort; got != want {
		t.Errorf("default MintBindAddress = %q, want %q", got, want)
	}
	t.Setenv(wiMintBindEnv, "0.0.0.0:9099")
	if got := MintBindAddress(); got != "0.0.0.0:9099" {
		t.Errorf("override MintBindAddress = %q, want 0.0.0.0:9099", got)
	}
}

func TestMintBindPort(t *testing.T) {
	cases := map[string]string{
		"":             defaultMintPort, // default :9080 -> 9080
		":9090":        "9090",
		"0.0.0.0:9091": "9091",
	}
	for bind, want := range cases {
		t.Setenv(wiMintBindEnv, bind)
		if got := mintBindPort(); got != want {
			t.Errorf("mintBindPort(bind=%q) = %q, want %q", bind, got, want)
		}
	}
}

func TestMintWorkloadToken_AudienceOverride(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		var body oidcTokenRequest
		_ = json.NewDecoder(req.Body).Decode(&body)
		if body.Audience != "override-aud" {
			t.Errorf("expected overridden audience, got %q", body.Audience)
		}
		_ = json.NewEncoder(w).Encode(oidcTokenResponse{Token: "t", ExpiresIn: 60})
	}))
	defer srv.Close()

	handle := "handle-override"
	wiStore.put(handle, []api.WorkloadIdentity{
		{Name: "ID", WorkloadToken: "wtok", Audience: "registered-aud"},
	}, srv.URL)
	defer wiStore.delete(handle)

	resp := MintWorkloadToken(context.Background(),
		api.MintWorkloadTokenRequest{Handle: handle, Name: "ID", AudienceOverride: "override-aud"})
	if resp.Error != "" {
		t.Fatalf("unexpected error: %s", resp.Error)
	}
}
