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
	// on Linux/Mac the mint URL is the bind-mounted Unix socket (no network port)
	if got, want := r.Envs[wiMintURLEnv], "unix://"+filepath.Join(spec.WISocketContainerDir, spec.WISocketName); got != want {
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
	} else if wi.WorkloadToken != "wtok" || tokenURL != "https://harnessid/token/generate" {
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
	if resp.Error == "" {
		t.Error("expected an error for unknown handle")
	}
	if resp.OidcToken != "" {
		t.Error("expected no token for unknown handle")
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
