// Copyright 2026 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

// Package binarydownload downloads a run step's plugin binary to a deterministic
// path and exports PLUGIN_PATH / PLUGIN_HOME / PLUGIN_DEFAULT_DL_PATH so a
// containerless plugin can run on a hosted VM. Supports public, multi-source
// (fallback) downloads, optionally zstd-compressed. Private-registry auth is not
// supported yet.
package binarydownload

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/harness/lite-engine/api"
	"github.com/harness/lite-engine/logger"
	"github.com/klauspost/compress/zstd"
)

const (
	taskType        = "plugin"
	downloadTimeout = 10 * time.Minute
	dirPerm         = 0o755 // download directories
	binPerm         = 0o755 // downloaded executable binary
)

// Setup downloads runConfig.Binary under baseDir and exports PLUGIN_HOME /
// PLUGIN_PATH / PLUGIN_DEFAULT_DL_PATH into envs.
func Setup(ctx context.Context, baseDir string, runConfig *api.RunConfig, envs map[string]string) error {
	if len(runConfig.Binary.Source) == 0 {
		return fmt.Errorf("binary source cannot be empty")
	}
	log := logger.FromContext(ctx)

	pluginDir := filepath.Join(baseDir, taskType)
	envs["PLUGIN_DEFAULT_DL_PATH"] = pluginDir

	urls := make([]string, 0, len(runConfig.Binary.Source))
	for _, source := range runConfig.Binary.Source {
		urls = append(urls, buildURL(source, runConfig.Binary.Version))
	}

	dest := resolveDest(runConfig.Binary, pluginDir, envs)
	log.Infof("Downloading plugin binary %q to %s", runConfig.Binary.Name, dest)

	binaryPath, err := download(ctx, urls, dest, runConfig.Binary.Compressed)
	if err != nil {
		return fmt.Errorf("failed to download binary %s: %w", runConfig.Binary.Name, err)
	}
	log.WithField("path", binaryPath).Infof("Binary ready at: %s", binaryPath)

	envs["PLUGIN_HOME"] = filepath.Dir(binaryPath)
	envs["PLUGIN_PATH"] = binaryPath

	// Rewrite the legacy ["plugin", "-kind", ...] entrypoint to the downloaded binary.
	if len(runConfig.Entrypoint) > 2 && runConfig.Entrypoint[0] == "plugin" && runConfig.Entrypoint[1] == "-kind" {
		runConfig.Entrypoint = []string{binaryPath}
		log.Infof("Updated entrypoint: %v", runConfig.Entrypoint)
	}
	return nil
}

// resolveDest returns the download destination: the expanded target if set,
// else a name/version/os/arch path.
func resolveDest(binary api.Binary, pluginDir string, envs map[string]string) string {
	if binary.Target != "" {
		return expandEnv(binary.Target, envs)
	}
	name := binary.Name
	file := fmt.Sprintf("%s-%s-%s-%s", name, binary.Version, runtime.GOOS, runtime.GOARCH)
	return filepath.Join(pluginDir, name, file)
}

// download returns dest on cache hit, else fetches from the first URL that succeeds.
// An artifact is compressed when compressed is set or the URL ends in ".zst"; it is
// fetched to dest+".zst" and decompressed into dest. The binary is marked executable.
func download(ctx context.Context, urls []string, dest string, compressed bool) (string, error) {
	log := logger.FromContext(ctx).WithField("target", dest)

	if _, err := os.Stat(dest); err == nil {
		log.Debug("cache hit")
		return dest, nil
	}
	log.Debug("cache miss")

	if err := os.MkdirAll(filepath.Dir(dest), dirPerm); err != nil {
		return "", err
	}

	client := &http.Client{
		Timeout: downloadTimeout, // overall budget for large binaries
		Transport: &http.Transport{
			DialContext: (&net.Dialer{
				Timeout:   30 * time.Second,
				KeepAlive: 30 * time.Second,
			}).DialContext,
			TLSHandshakeTimeout:   10 * time.Second,
			ResponseHeaderTimeout: 30 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
		},
	}
	var lastErr error
	for _, url := range urls {
		// Infer ".zst" per URL so an unset compressed flag still decompresses.
		isCompressed := compressed || strings.HasSuffix(url, ".zst")

		downloadPath := dest
		if isCompressed {
			downloadPath = dest + ".zst"
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			lastErr = fmt.Errorf("failed to create request for %s: %w", url, err)
			continue
		}
		resp, err := client.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("failed to download from %s: %w", url, err)
			continue
		}
		if resp.StatusCode < 200 || resp.StatusCode > 299 {
			resp.Body.Close()
			lastErr = fmt.Errorf("download error status %d for url %s", resp.StatusCode, url)
			continue
		}

		out, err := os.Create(downloadPath)
		if err != nil {
			resp.Body.Close()
			return "", fmt.Errorf("failed to create file: %w", err)
		}
		_, err = io.Copy(out, resp.Body)
		out.Close()
		resp.Body.Close()
		if err != nil {
			os.Remove(downloadPath) // drop partial file so it can't poison the cache on the next run
			return "", fmt.Errorf("failed to write file: %w", err)
		}

		binPath := downloadPath
		if isCompressed {
			if err := decompressZst(ctx, downloadPath, dest); err != nil {
				os.Remove(downloadPath) // partial .zst
				os.Remove(dest)         // partial decompressed output
				return "", fmt.Errorf("failed to decompress %s: %w", downloadPath, err)
			}
			binPath = dest
		}

		if err := os.Chmod(binPath, binPerm); err != nil {
			os.Remove(binPath)
			return "", fmt.Errorf("failed to set executable flag on %s: %w", binPath, err)
		}
		return binPath, nil
	}
	return "", fmt.Errorf("failed to download from all urls: %w", lastErr)
}

// decompressZst decompresses the zstd file at src into dst and removes src on success.
func decompressZst(ctx context.Context, src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("failed to open compressed file: %w", err)
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return fmt.Errorf("failed to create decompressed file: %w", err)
	}
	defer out.Close()

	reader, err := zstd.NewReader(in)
	if err != nil {
		return err
	}
	defer reader.Close()

	if _, err := io.Copy(out, reader); err != nil { //nolint:gosec
		return err
	}

	if err := os.Remove(src); err != nil {
		logger.FromContext(ctx).Errorf("failed to remove compressed file %s after decompression: %v", src, err)
	}
	return nil
}

// buildURL substitutes os/arch tokens always (they don't depend on the version) and
// the release token only when release is set, so ".../plugin-{{ os }}-{{ arch }}.zst"
// works with no version.
func buildURL(template, release string) string {
	replacements := []string{
		"{{ os }}", runtime.GOOS,
		"{{ arch }}", runtime.GOARCH,
	}
	if release != "" {
		replacements = append(replacements, "{{ release }}", release)
	}
	return strings.NewReplacer(replacements...).Replace(template)
}

// expandEnv resolves ${VAR} in s against envs then the OS env, in a few passes
// to handle nested references.
func expandEnv(s string, envs map[string]string) string {
	expand := func(key string) string {
		if val, ok := envs[key]; ok {
			return val
		}
		return os.Getenv(key)
	}
	for i := 0; i < 3; i++ {
		s = os.Expand(s, expand)
	}
	return s
}
