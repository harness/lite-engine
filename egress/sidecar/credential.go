// Package sidecar is the production egress sidecar for hosted Linux build VMs
// (CI-26162). It rewrites client traffic into credentialed requests for the
// fleet proxy. Nothing starts it yet; the install story (CI-26163) does that.
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

// ReadCredentialFile reads the credential once, ignoring surrounding whitespace.
func ReadCredentialFile(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return ParseCredential(string(bytes.TrimSpace(b)))
}

// BasicAuth returns the Proxy-Authorization value for a validated credential.
func BasicAuth(credential string) string {
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(credential))
}
