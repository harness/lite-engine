// Copyright 2026 Harness Inc. All rights reserved.
// Use of this source code is governed by the PolyForm Free Trial 1.0.0 license
// that can be found in the licenses directory at the root of this repository, also available at
// https://polyformproject.org/wp-content/uploads/2020/05/PolyForm-Free-Trial-1.0.0.txt.

package sidecar

import (
	"bytes"
	"encoding/base64"
	"errors"
	"os"
	"strings"
)

// ErrCredentialIncomplete means the credential is missing, has no colon, or has
// an empty username or password. The message never includes the credential.
var ErrCredentialIncomplete = errors.New("proxy credential missing or incomplete")

// ParseCredential validates a "user:password" credential. The first colon
// separates the username from the password.
func ParseCredential(raw string) (string, error) {
	user, password, ok := strings.Cut(raw, ":")
	if !ok || user == "" || password == "" {
		return "", ErrCredentialIncomplete
	}
	return raw, nil
}

// readCredentialFile reads the credential once, ignoring surrounding whitespace.
func readCredentialFile(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return ParseCredential(string(bytes.TrimSpace(b)))
}

// basicAuth returns the Proxy-Authorization value for a validated credential.
func basicAuth(credential string) string {
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(credential))
}
