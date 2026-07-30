// Copyright 2026 Harness Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package runtime

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/harness/lite-engine/api"
	"github.com/sirupsen/logrus"
)

const (
	// wiHandleEnv is injected into the step container. hcli reads it and passes it back to the mint
	// endpoint; it is a non-secret capability reference, not the workload token.
	wiHandleEnv = "HARNESS_WI_HANDLE"
	// wiMintURLEnv tells hcli where lite-engine's mint endpoint is reachable from inside the step.
	wiMintURLEnv = "HARNESS_WI_MINT_URL"
	// wiMintURLOverrideEnv lets the lite-engine process override the mint URL handed to steps (used to
	// tune container->host reachability per VM/OS without a rebuild).
	wiMintURLOverrideEnv = "HARNESS_WI_MINT_URL_OVERRIDE"
	// workloadTokenHeader carries the opaque workload token when calling HarnessID generate.
	workloadTokenHeader = "Workload-Token"
	// hostGatewayExtraHost lets the step container resolve the VM host (where lite-engine listens).
	hostGatewayExtraHost = "host.docker.internal:host-gateway"
	// defaultMintURL is the address a step container uses to reach lite-engine's mint endpoint. The
	// container reaches the VM host via host.docker.internal (host-gateway). This targets the dedicated
	// PLAIN-HTTP mint listener (port 9080), not the main mTLS server (9079) - the step container cannot
	// present the mTLS client cert, and the opaque handle is the capability that authorizes the mint.
	// Override via HARNESS_WI_MINT_URL_OVERRIDE if the port/host differs.
	defaultMintURL = "http://host.docker.internal:9080/mint_workload_token"
)

// wiEntry is the per-handle registration: the identities the step may mint, plus the HarnessID
// token-generate endpoint to broker against (both delivered on the step contract by ci-manager).
type wiEntry struct {
	identities map[string]api.WorkloadIdentity
	tokenURL   string
}

// workloadIdentityStore holds, per opaque per-step handle, the registered identities. The workload
// token stays inside lite-engine and is never exposed to the step environment; only the handle and the
// mint URL are injected. Unlike the K8s engine (whose process dies with the pod), lite-engine outlives
// a single step on a VM, so entries are evicted when the step completes.
type workloadIdentityStore struct {
	mu      sync.RWMutex
	entries map[string]*wiEntry
}

var wiStore = &workloadIdentityStore{entries: make(map[string]*wiEntry)}

func (s *workloadIdentityStore) put(handle string, identities []api.WorkloadIdentity, tokenURL string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m := make(map[string]api.WorkloadIdentity, len(identities))
	for _, wi := range identities {
		if wi.Name != "" {
			m[wi.Name] = wi
		}
	}
	s.entries[handle] = &wiEntry{identities: m, tokenURL: tokenURL}
}

func (s *workloadIdentityStore) get(handle, name string) (api.WorkloadIdentity, string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	e, ok := s.entries[handle]
	if !ok {
		return api.WorkloadIdentity{}, "", false
	}
	wi, ok := e.identities[name]
	return wi, e.tokenURL, ok
}

func (s *workloadIdentityStore) delete(handle string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.entries, handle)
}

func newWorkloadIdentityHandle() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// registerWorkloadIdentities stores the step's identities under a fresh handle and injects
// HARNESS_WI_HANDLE + HARNESS_WI_MINT_URL into the step environment (plus a host-gateway extra host so
// the container can reach lite-engine). The workload tokens themselves are never written to the step
// env. Returns the handle (empty when the step declares no identities) so the caller can evict it once
// the step completes.
func registerWorkloadIdentities(r *api.StartStepRequest) string {
	if r == nil || len(r.WorkloadIdentities) == 0 {
		return ""
	}
	handle, err := newWorkloadIdentityHandle()
	if err != nil {
		logrus.WithError(err).Errorln("workload-identity: failed to generate handle; dropping identities")
		r.WorkloadIdentities = nil
		return ""
	}
	wiStore.put(handle, r.WorkloadIdentities, r.WITokenGenerateURL)

	if r.Envs == nil {
		r.Envs = map[string]string{}
	}
	r.Envs[wiHandleEnv] = handle
	r.Envs[wiMintURLEnv] = mintURL()
	r.ExtraHosts = appendIfMissing(r.ExtraHosts, hostGatewayExtraHost)

	// Strip the sensitive workload tokens before the request is used to build the container step.
	r.WorkloadIdentities = nil
	logrus.WithField("id", r.ID).Infoln("workload-identity: registered identities for step")
	return handle
}

func mintURL() string {
	if o := os.Getenv(wiMintURLOverrideEnv); o != "" {
		return o
	}
	return defaultMintURL
}

func appendIfMissing(hosts []string, val string) []string {
	for _, h := range hosts {
		if h == val {
			return hosts
		}
	}
	return append(hosts, val)
}

// MintWorkloadToken resolves the per-step handle to the held workload token and brokers an OIDC mint
// against HarnessID. The workload token never leaves lite-engine.
func MintWorkloadToken(ctx context.Context, req api.MintWorkloadTokenRequest) api.MintWorkloadTokenResponse {
	wi, tokenURL, ok := wiStore.get(req.Handle, req.Name)
	if !ok {
		logrus.WithField("name", req.Name).Warnln("workload-identity: unknown handle/name for mint")
		return api.MintWorkloadTokenResponse{Error: "unknown workload identity"}
	}
	audience := wi.Audience
	if req.AudienceOverride != "" {
		audience = req.AudienceOverride
	}
	token, expiresAt, err := mintOidcToken(ctx, tokenURL, wi.WorkloadToken, audience, wi.TokenMode)
	if err != nil {
		logrus.WithError(err).WithField("name", req.Name).Errorln("workload-identity: mint failed")
		return api.MintWorkloadTokenResponse{Error: err.Error()}
	}
	return api.MintWorkloadTokenResponse{OidcToken: token, ExpiresAtUnix: expiresAt}
}

type oidcTokenRequest struct {
	Audience  string `json:"audience"`
	TokenMode string `json:"token_mode,omitempty"`
}

type oidcTokenResponse struct {
	Token     string `json:"token"`
	ExpiresIn int64  `json:"expires_in"`
}

var harnessIDHTTPClient = &http.Client{Timeout: 15 * time.Second}

// mintOidcToken POSTs to the HarnessID OIDC token-generate endpoint. The generate call is authenticated
// solely by the Workload-Token header, matching the HarnessID contract.
func mintOidcToken(ctx context.Context, tokenURL, workloadToken, audience, tokenMode string) (string, int64, error) {
	if tokenURL == "" {
		return "", 0, fmt.Errorf("workload identity token-generate URL is not set for this step")
	}
	body, err := json.Marshal(oidcTokenRequest{Audience: audience, TokenMode: tokenMode})
	if err != nil {
		return "", 0, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, bytes.NewReader(body))
	if err != nil {
		return "", 0, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set(workloadTokenHeader, workloadToken)
	resp, err := harnessIDHTTPClient.Do(httpReq)
	if err != nil {
		return "", 0, err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", 0, fmt.Errorf("HarnessID token generation failed: status %d: %s", resp.StatusCode, string(respBody))
	}
	var parsed oidcTokenResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return "", 0, err
	}
	if parsed.Token == "" {
		return "", 0, fmt.Errorf("HarnessID returned an empty OIDC token")
	}
	var expiresAt int64
	if parsed.ExpiresIn > 0 {
		expiresAt = time.Now().Unix() + parsed.ExpiresIn
	}
	return parsed.Token, expiresAt, nil
}
