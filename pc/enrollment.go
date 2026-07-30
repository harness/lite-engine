// Copyright 2026 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package pc

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	enrollmentExchangePath = "/ng/api/private-connectivity/enrollment/exchange"
	enrollmentTimeout      = 15 * time.Second
	maxEnrollmentResponse  = 1 << 20
	enrollmentDialTimeout  = 10 * time.Second
	enrollmentKeepAlive    = 30 * time.Second
	enrollmentTLSTimeout   = 10 * time.Second
)

type enrollmentEnvelope struct {
	Data struct {
		ClientID          string `json:"clientId"`
		IDToken           string `json:"idToken"`
		Hostname          string `json:"hostname"`
		Tag               string `json:"tag"`
		BindingGeneration uint64 `json:"bindingGeneration"`
		ExpiresAt         int64  `json:"expiresAt"`
	} `json:"data"`
}

// ExchangeEnrollment exchanges the one-time ticket without accepting any
// account, tag, generation, audience, or hostname from the setup request.
func ExchangeEnrollment(ctx context.Context, endpoint, ticket string) (Config, error) {
	if strings.TrimSpace(ticket) == "" {
		return Config{}, fmt.Errorf("pc: enrollment ticket is required")
	}
	exchangeURL, err := validateExchangeURL(endpoint)
	if err != nil {
		return Config{}, err
	}
	payload, err := json.Marshal(map[string]string{"ticket": ticket})
	if err != nil {
		return Config{}, fmt.Errorf("pc: failed to encode enrollment request")
	}
	exchangeCtx, cancel := context.WithTimeout(ctx, enrollmentTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(
		exchangeCtx, http.MethodPost, exchangeURL.String(), bytes.NewReader(payload))
	if err != nil {
		return Config{}, fmt.Errorf("pc: failed to create enrollment request")
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")

	client := &http.Client{
		// Enrollment is control-plane traffic and must not inherit
		// customer-provided proxy environment variables.
		Transport: &http.Transport{
			DialContext: (&net.Dialer{
				Timeout:   enrollmentDialTimeout,
				KeepAlive: enrollmentKeepAlive,
			}).DialContext,
			TLSClientConfig:     &tls.Config{MinVersion: tls.VersionTLS12},
			TLSHandshakeTimeout: enrollmentTLSTimeout,
		},
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	response, err := client.Do(request)
	if err != nil {
		return Config{}, fmt.Errorf("pc: enrollment exchange failed")
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, maxEnrollmentResponse))
	if err != nil {
		return Config{}, fmt.Errorf("pc: enrollment exchange failed")
	}
	if response.StatusCode != http.StatusOK {
		return Config{}, fmt.Errorf("pc: enrollment exchange rejected")
	}
	if !strings.Contains(strings.ToLower(response.Header.Get("Cache-Control")), "no-store") {
		return Config{}, fmt.Errorf("pc: enrollment exchange response is not non-cacheable")
	}
	var envelope enrollmentEnvelope
	if err := json.Unmarshal(body, &envelope); err != nil {
		return Config{}, fmt.Errorf("pc: enrollment exchange returned an invalid response")
	}
	cfg := Config{
		Enabled:           true,
		ClientID:          envelope.Data.ClientID,
		OIDCToken:         envelope.Data.IDToken,
		Hostname:          envelope.Data.Hostname,
		Tag:               envelope.Data.Tag,
		BindingGeneration: envelope.Data.BindingGeneration,
		ExpiresAt:         time.UnixMilli(envelope.Data.ExpiresAt),
	}
	if err := ValidateExchange(&cfg, time.Now()); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func validateExchangeURL(value string) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, fmt.Errorf("pc: enrollment exchange URL is invalid")
	}
	if !strings.HasSuffix(strings.TrimRight(parsed.Path, "/"), enrollmentExchangePath) {
		return nil, fmt.Errorf("pc: enrollment exchange URL has an invalid path")
	}
	if parsed.Scheme == "https" {
		return parsed, nil
	}
	host := parsed.Hostname()
	if parsed.Scheme == "http" && (strings.EqualFold(host, "localhost") || net.ParseIP(host).IsLoopback()) {
		return parsed, nil
	}
	return nil, fmt.Errorf("pc: enrollment exchange URL must use HTTPS")
}
