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
	"path/filepath"
	goruntime "runtime"
	"strings"
	"sync"
	"time"

	"github.com/harness/lite-engine/api"
	"github.com/harness/lite-engine/engine/spec"
	"github.com/sirupsen/logrus"
)

const (
	// wiHandleEnv is injected into the step container. hcli reads it and passes it back to the mint
	// endpoint; it is a non-secret capability reference, not the workload token. Shared with the docker
	// package (which uses it to decide whether to bind-mount the socket) via spec.WIHandleEnv.
	wiHandleEnv = spec.WIHandleEnv
	// ErrUnknownWorkloadIdentity is returned when a mint request's handle/name does not resolve. It is a
	// not-found condition (the handler maps it to HTTP 404), not an internal mint failure.
	ErrUnknownWorkloadIdentity = "unknown workload identity"
	// wiMintURLEnv tells hcli where lite-engine's mint endpoint is reachable from inside the step.
	wiMintURLEnv = "HARNESS_WI_MINT_URL"
	// wiMintURLOverrideEnv lets the lite-engine process override the mint URL handed to steps (used to
	// tune container->host reachability per VM/OS without a rebuild).
	wiMintURLOverrideEnv = "HARNESS_WI_MINT_URL_OVERRIDE"
	// workloadTokenHeader carries the opaque workload token when calling HarnessID generate.
	workloadTokenHeader = "Workload-Token"
	// hostGatewayExtraHost lets the step container resolve the VM host as a fallback when the docker
	// network gateway IP is not available.
	hostGatewayExtraHost = "host.docker.internal:host-gateway"
	// wiMintBindEnv is the address lite-engine's Windows TCP mint listener binds to (Linux/Mac use the unix
	// socket instead). The injected mint URL derives its port from this SAME var so the listener and the URL
	// cannot drift; HARNESS_WI_MINT_URL_OVERRIDE still overrides the whole URL when set.
	wiMintBindEnv = "HARNESS_WI_MINT_BIND"
	// defaultMintPort is the default TCP port for the Windows mint listener.
	defaultMintPort = "9080"
	// windowsOS is the GOOS value for Windows; Linux/Mac use the unix socket, Windows the TCP fallback.
	windowsOS = "windows"
	// handleRandomBytes is the size of an opaque per-step handle (128-bit).
	handleRandomBytes = 16
	// harnessIDHTTPTimeout bounds the HarnessID token-generate call.
	harnessIDHTTPTimeout = 15 * time.Second
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

func (s *workloadIdentityStore) clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries = make(map[string]*wiEntry)
}

// ClearWorkloadIdentities drops all registered workload identities. Called at stage teardown (the Destroy
// handler), after every step - including detached daemons, which are not evicted on step completion - has
// stopped. This is the backstop that guarantees no workload token or mintable handle outlives the stage,
// even on a pooled VM where the lite-engine process is reused across stages.
func ClearWorkloadIdentities() {
	wiStore.clear()
}

func newWorkloadIdentityHandle() (string, error) {
	b := make([]byte, handleRandomBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// registerWorkloadIdentities stores the step's identities under a fresh handle and injects
// HARNESS_WI_HANDLE + HARNESS_WI_MINT_URL into the step environment. On Linux/Mac the mint URL points
// at the Unix socket at spec.WISocketDir, which lite-engine bind-mounts into container steps at the
// SAME path - so the same URL works whether the step runs in a container or directly on the host
// (containerless). Windows uses the host.docker.internal TCP fallback. The workload tokens themselves
// are never written to the step env. Returns the handle (empty when the step declares no identities).
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
	mint := mintURL()
	r.Envs[wiHandleEnv] = handle
	r.Envs[wiMintURLEnv] = mint
	// Only the Windows TCP fallback needs host.docker.internal; the Linux/Mac Unix socket does not.
	if goruntime.GOOS == windowsOS {
		r.ExtraHosts = appendIfMissing(r.ExtraHosts, hostGatewayExtraHost)
	}

	// Strip the sensitive workload tokens before the request is used to build the container step.
	r.WorkloadIdentities = nil
	logrus.WithField("id", r.ID).WithField("mint_url", mint).Infoln("workload-identity: registered identities for step")
	return handle
}

// setupWorkloadIdentity registers the step's declared identities (injecting the handle + mint URL) and
// returns a cleanup that evicts the handle when the step completes - bounding memory and revoking minting
// on a pooled VM. A detached step keeps running after executeStep returns, so its handle is left in place
// (cleared at stage teardown by ClearWorkloadIdentities in the Destroy handler) and the cleanup is a no-op.
func setupWorkloadIdentity(r *api.StartStepRequest) func() {
	handle := registerWorkloadIdentities(r)
	if handle == "" || r.Detach {
		return func() {}
	}
	return func() { wiStore.delete(handle) }
}

// mintURL builds the mint endpoint the in-step hcli should call. Priority: explicit override env ->
// the Unix socket at spec.WISocketDir (Linux/Mac; no port/firewall/mTLS/DNS). The socket lives on the
// VM host and is bind-mounted into container steps at the SAME path, so this single URL is valid both
// for container steps and for containerless (host-executed) steps. Windows falls back to
// host.docker.internal TCP (best-effort).
func mintURL() string {
	if o := os.Getenv(wiMintURLOverrideEnv); o != "" {
		return o
	}
	if goruntime.GOOS != windowsOS {
		return "unix://" + filepath.Join(spec.WISocketDir, spec.WISocketName)
	}
	// Windows fallback (no unix sockets in Windows containers): the step reaches lite-engine over TCP via
	// host.docker.internal. Derive the port from the same bind the listener uses so changing the bind can
	// never leave the injected URL pointing at the wrong port.
	return fmt.Sprintf("http://host.docker.internal:%s/mint_workload_token", mintBindPort())
}

// MintBindAddress is the listen address for the Windows TCP mint server (default :9080), overridable via
// HARNESS_WI_MINT_BIND. Both the listener (cli/server) and the injected mint URL derive from this single
// source so they always agree.
func MintBindAddress() string {
	if b := os.Getenv(wiMintBindEnv); b != "" {
		return b
	}
	return ":" + defaultMintPort
}

// mintBindPort returns just the port from MintBindAddress, used to build the mint URL handed to the step.
func mintBindPort() string {
	addr := MintBindAddress()
	if i := strings.LastIndex(addr, ":"); i >= 0 && i+1 < len(addr) {
		return addr[i+1:]
	}
	return defaultMintPort
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
		return api.MintWorkloadTokenResponse{Error: ErrUnknownWorkloadIdentity}
	}
	audience := wi.Audience
	if req.AudienceOverride != "" {
		audience = req.AudienceOverride
	}
	token, expiresAt, err := mintOidcToken(ctx, tokenURL, wi.WorkloadToken, audience, wi.TokenMode)
	if err != nil {
		// Log the detailed cause (which may carry HarnessID's raw error body) server-side only. Return a
		// generic message to the step: the step is untrusted and must not see HarnessID's internal payload.
		logrus.WithError(err).WithField("name", req.Name).Errorln("workload-identity: mint failed")
		return api.MintWorkloadTokenResponse{Error: "failed to mint workload identity token"}
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

var harnessIDHTTPClient = &http.Client{Timeout: harnessIDHTTPTimeout}

// mintOidcToken POSTs to the HarnessID OIDC token-generate endpoint. The generate call is authenticated
// solely by the Workload-Token header, matching the HarnessID contract.
func mintOidcToken(ctx context.Context, tokenURL, workloadToken, audience, tokenMode string) (token string, expiresAtUnix int64, err error) {
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
