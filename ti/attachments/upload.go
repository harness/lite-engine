// Copyright 2026 Harness Inc. All rights reserved.
// Use of this source code is governed by the PolyForm Free Trial 1.0.0 license
// that can be found in the licenses directory at the root of this repository, also available at
// https://polyformproject.org/wp-content/uploads/2020/05/PolyForm-Free-Trial-1.0.0.txt.

package attachments

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	destinationDir  = "test-attachments"
	uploadWorkers   = 4
	uploadAttempts  = 3
	maxReportedErrs = 20
	authPrefix      = "CIManager"
	dialTimeout     = 30 * time.Second
	tlsTimeout      = 10 * time.Second
	headerTimeout   = 60 * time.Second
	idleTimeout     = 90 * time.Second
	clientTimeout   = 10 * time.Minute
	backoffFactor   = 2
	maxErrorBody    = 4096
)

var (
	httpClient    = newHTTPClient()
	retryBackoff  = 2 * time.Second
	errorsOmitted = "further errors omitted"
)

func newHTTPClient() *http.Client {
	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           (&net.Dialer{Timeout: dialTimeout, KeepAlive: dialTimeout}).DialContext,
		TLSHandshakeTimeout:   tlsTimeout,
		ResponseHeaderTimeout: headerTimeout,
		IdleConnTimeout:       idleTimeout,
	}
	return &http.Client{
		Timeout:   clientTimeout,
		Transport: transport,
		// A redirect would resend the token and the body to wherever it points, so it is never followed.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

// upload names a file by its path relative to the resolved workspace root. The file is opened through that
// root at upload time, so a path component the step swaps for a symlink after the check cannot lead out of it.
type upload struct {
	sourcePath   string
	rootDir      string
	relPath      string
	artifactPath string
}

type permanentError struct{ cause error }

func (e *permanentError) Error() string { return e.cause.Error() }
func (e *permanentError) Unwrap() error { return e.cause }

// resolveUploads accepts only regular files whose real path is inside the workspace. It returns the
// uploads and one error per rejected file.
func resolveUploads(workspace, uploadRoot string, files []file) (uploads []upload, invalid []error) {
	root, err := filepath.EvalSymlinks(workspace)
	if err != nil {
		return nil, []error{fmt.Errorf("workspace %s: %w", workspace, err)}
	}
	seen := make(map[string]bool, len(files))
	for _, f := range files {
		dest, err := cleanDestination(f.destination)
		if err != nil {
			invalid = append(invalid, err)
			continue
		}
		artifactPath := path.Join(uploadRoot, dest)
		if seen[artifactPath] {
			invalid = append(invalid, fmt.Errorf("%s: duplicate destination %q, skipped", f.sourcePath, dest))
			continue
		}
		rel, err := resolveWithinRoot(root, f.sourcePath)
		if err != nil {
			invalid = append(invalid, err)
			continue
		}
		seen[artifactPath] = true
		uploads = append(uploads, upload{sourcePath: f.sourcePath, rootDir: root, relPath: rel, artifactPath: artifactPath})
	}
	return uploads, invalid
}

// resolveWithinRoot returns the path of src relative to root, if src is a regular file inside it.
func resolveWithinRoot(root, src string) (string, error) {
	resolved, err := filepath.EvalSymlinks(src)
	if err != nil {
		return "", fmt.Errorf("%s: %w", src, err)
	}
	if !isWithin(root, resolved) {
		return "", fmt.Errorf("%s: not inside the workspace", src)
	}
	rel, err := filepath.Rel(root, resolved)
	if err != nil {
		return "", fmt.Errorf("%s: %w", src, err)
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", fmt.Errorf("%s: %w", src, err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("%s: not a regular file", src)
	}
	if info.Size() > MaxFileBytes {
		return "", fmt.Errorf("%s: %d bytes exceed the per-file limit of %d", src, info.Size(), MaxFileBytes)
	}
	return rel, nil
}

func cleanDestination(dest string) (string, error) {
	if dest == "" || strings.HasPrefix(dest, "/") || strings.Contains(dest, "\\") {
		return "", fmt.Errorf("invalid destination %q", dest)
	}
	for _, s := range strings.Split(dest, "/") {
		if s == "" || s == "." || s == ".." {
			return "", fmt.Errorf("invalid destination %q", dest)
		}
	}
	return dest, nil
}

// uploadAll uploads every file and returns one entry per upload, nil for the ones that succeeded.
func uploadAll(ctx context.Context, cfg *Config, uploads []upload) []error {
	if len(uploads) == 0 {
		return nil
	}
	workers := uploadWorkers
	if len(uploads) < workers {
		workers = len(uploads)
	}
	jobs := make(chan upload, len(uploads))
	results := make(chan error, len(uploads))

	var wg sync.WaitGroup
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			for u := range jobs {
				if ctx.Err() != nil {
					results <- fmt.Errorf("%s: %w", u.artifactPath, ctx.Err())
					continue
				}
				results <- uploadWithRetry(ctx, cfg, u)
			}
		}()
	}
	for _, u := range uploads {
		jobs <- u
	}
	close(jobs)
	wg.Wait()
	close(results)

	out := make([]error, 0, len(uploads))
	for r := range results {
		out = append(out, r)
	}
	return out
}

func uploadWithRetry(ctx context.Context, cfg *Config, u upload) error {
	backoff := retryBackoff
	var lastErr error
	for attempt := 1; attempt <= uploadAttempts; attempt++ {
		err := uploadOne(ctx, cfg, u)
		if err == nil {
			return nil
		}
		lastErr = err
		var permErr *permanentError
		if errors.As(err, &permErr) || errors.Is(err, os.ErrNotExist) || errors.Is(err, os.ErrPermission) {
			break
		}
		if attempt < uploadAttempts {
			select {
			case <-ctx.Done():
				return fmt.Errorf("%s: %w", u.artifactPath, ctx.Err())
			case <-time.After(backoff + jitter(backoff/backoffFactor)):
			}
			backoff *= backoffFactor
		}
	}
	return fmt.Errorf("%s: %w", u.artifactPath, lastErr)
}

func uploadOne(ctx context.Context, cfg *Config, u upload) error {
	pushURL, err := buildPushURL(cfg.URL, cfg.AccountID, cfg.Registry, u.artifactPath)
	if err != nil {
		return &permanentError{err}
	}

	// os.Root refuses to follow a symlink out of the workspace, which the path check done earlier cannot
	// guarantee once the step is free to change the tree.
	root, err := os.OpenRoot(u.rootDir)
	if err != nil {
		return err
	}
	defer root.Close()
	f, err := root.Open(u.relPath)
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return &permanentError{fmt.Errorf("%s: not a regular file", u.sourcePath)}
	}
	if info.Size() > MaxFileBytes {
		return &permanentError{fmt.Errorf("%s: %d bytes exceed the per-file limit of %d", u.sourcePath, info.Size(), MaxFileBytes)}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, pushURL, f)
	if err != nil {
		return &permanentError{err}
	}
	req.ContentLength = info.Size()
	// Content-Type stays unset: HAR stores a non-empty value as-is and only falls back to the file extension
	// when it is empty, which preview depends on.
	req.Header.Set("Authorization", authPrefix+" "+cfg.Token)

	resp, err := httpClient.Do(req)
	if err != nil {
		return sanitizeError(err, cfg.Token)
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))
		err := fmt.Errorf("upload rejected with %s: %s", resp.Status, strings.TrimSpace(string(body)))
		if resp.StatusCode != http.StatusTooManyRequests && resp.StatusCode < http.StatusInternalServerError {
			return &permanentError{err}
		}
		return err
	}
	return nil
}

// sanitizeError keeps the token out of transport errors, which can embed the request URL or headers.
func sanitizeError(err error, token string) error {
	if token == "" || !strings.Contains(err.Error(), token) {
		return err
	}
	return errors.New(strings.ReplaceAll(err.Error(), token, "***"))
}

func buildPushURL(baseURL, accountID, registry, artifactPath string) (string, error) {
	baseURL = strings.TrimSuffix(strings.TrimSpace(baseURL), "/")
	if !strings.Contains(baseURL, "://") {
		baseURL = "https://" + baseURL
	}
	// url.JoinPath treats every element as already escaped, so escape them here to keep names such as
	// "100%.png" or "a%2Fb" exactly.
	segments := []string{"pkg", url.PathEscape(accountID), url.PathEscape(registry), "files"}
	for _, s := range strings.Split(artifactPath, "/") {
		if s != "" {
			segments = append(segments, url.PathEscape(s))
		}
	}
	u, err := url.JoinPath(baseURL, segments...)
	if err != nil {
		return "", fmt.Errorf("invalid upload URL: %w", err)
	}
	return u, nil
}

// jitter returns a random duration in [0, limit] to spread out retries.
func jitter(limit time.Duration) time.Duration {
	n, err := rand.Int(rand.Reader, big.NewInt(int64(limit)+1))
	if err != nil {
		return 0
	}
	return time.Duration(n.Int64())
}
