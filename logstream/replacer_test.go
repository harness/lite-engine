// Copyright 2022 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package logstream

import (
	"testing"
)

func TestReplace(t *testing.T) {
	secrets := []string{"correct-horse-batter-staple", ""}

	sw := &nopWriter{}
	w := NewReplacer(&nopCloser{sw}, secrets)
	_, _ = w.Write([]byte("username octocat password correct-horse-batter-staple"))
	w.Close()

	if got, want := sw.data[0], "username octocat password **************"; got != want {
		t.Errorf("Want masked string %s, got %s", want, got)
	}
}

func TestReplaceMultiline(t *testing.T) {
	key := `
-----BEGIN PRIVATE KEY-----
MIIBVQIBADANBgkqhkiG9w0BAQEFAASCAT8wggE7AgEAAkEA0SC5BIYpanOv6wSm
dHVVMRa+6iw/0aJpT9/LKcZ0XYQ43P9Vwn8c46MDvFJ+Uy41FwbxT+QpXBoLlp8D
sJY/dQIDAQABAkAesoL2GwtxSNIF2YTli2OZ9RDJJv2nNAPpaZxU4YCrST1AXGPB
tFm0LjYDDlGJ448syKRpdypAyCR2LidwrVRxAiEA+YU5Zv7bOwODCsmtQtIfBfhu
6SMBGMDijK7OYfTtjQsCIQDWjvly6b6doVMdNjqqTsnA8J1ShjSb8bFXkMels941
fwIhAL4Rr7I3PMRtXmrfSa325U7k+Yd59KHofCpyFiAkNLgVAiB8JdR+wnOSQAOY
loVRgC9LXa6aTp9oUGxeD58F6VK9PwIhAIDhSxkrIatXw+dxelt8DY0bEdDbYzky
r9nicR5wDy2W
-----END PRIVATE KEY-----`

	line := `> MIIBVQIBADANBgkqhkiG9w0BAQEFAASCAT8wggE7AgEAAkEA0SC5BIYpanOv6wSm`

	secrets := []string{key}

	sw := &nopWriter{}
	w := NewReplacer(&nopCloser{sw}, secrets)
	_, _ = w.Write([]byte(line))
	w.Close()

	if got, want := sw.data[0], "> **************"; got != want {
		t.Errorf("Want masked string %s, got %s", want, got)
	}
}

func TestReplaceMultilineJson(t *testing.T) {
	key := `{
  "token":"MIIBVQIBADANBgkqhkiG9w0BAQEFAASCAT8wggE7AgEAAkEA0SC5BIYpanOv6wSm"
}`

	line := `{
  "token":"MIIBVQIBADANBgkqhkiG9w0BAQEFAASCAT8wggE7AgEAAkEA0SC5BIYpanOv6wSm"
}`

	secrets := []string{key}

	sw := &nopWriter{}
	w := NewReplacer(&nopCloser{sw}, secrets)
	_, _ = w.Write([]byte(line))
	w.Close()

	if got, want := sw.data[0], "{\n  **************\n}"; got != want {
		t.Errorf("Want masked string %s, got %s", want, got)
	}
}

// Test functions for createSecretVariants
func TestCreateSecretVariants_Essential(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{
			name:     "JSON with quotes",
			input:    `{"token": "secret123"}`,
			expected: []string{`{"token": "secret123"}`, `{token: secret123}`, `{"token":"secret123"}`},
		},
		{
			name:     "Shell variable",
			input:    "key=$USER_TOKEN",
			expected: []string{"key=$USER_TOKEN", "key="},
		},
		{
			name:     "Command substitution",
			input:    "value=`date +%Y`",
			expected: []string{"value=`date +%Y`", "value="},
		},
		{
			name:     "Double quote stripping",
			input:    `"secret123"`,
			expected: []string{`"secret123"`, "secret123"},
		},
		{
			name:     "Single quote stripping",
			input:    "'secret123'",
			expected: []string{"'secret123'", "secret123"},
		},
		{
			name:     "URL encoding variants",
			input:    "hello world",
			expected: []string{"hello world", "hello+world", "hello%20world"},
		},
		{
			name:     "Complex JSON",
			input:    `{"message": "Hello World", "token": "abc123"}`,
			expected: []string{`{"message": "Hello World", "token": "abc123"}`, `{message: Hello World, token: abc123}`, `{"message":"Hello World","token":"abc123"}`},
		},
		{
			name:     "Escaped quotes",
			input:    `{"text": "He said \"Hello\""}`,
			expected: []string{`{"text": "He said \"Hello\""}`, `{"text": "He said Hello"}`},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := createSecretVariants(tt.input)
			// Check that all expected variants are present
			for _, expected := range tt.expected {
				found := false
				for _, variant := range result {
					if variant == expected {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("Expected variant %v not found in %v", expected, result)
				}
			}
		})
	}
}

func TestCompactNonStringWhitespace_Essential(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "Basic JSON compaction",
			input:    `{ "key": "value" }`,
			expected: `{"key":"value"}`,
		},
		{
			name:     "Preserve spaces in strings",
			input:    `{"message": "Hello World"}`,
			expected: `{"message":"Hello World"}`,
		},
		{
			name:     "Handle escaped quotes",
			input:    `{"text": "He said \"Hello\""}`,
			expected: `{"text":"He said \"Hello\""}`,
		},
		{
			name:     "Complex nested JSON",
			input:    `{ "outer": { "inner": "value" }, "array": [ "item1", "item2" ] }`,
			expected: `{"outer":{"inner":"value"},"array":["item1","item2"]}`,
		},
		{
			name:     "Remove tabs and newlines",
			input:    "{\n\t\"key\": \"value\"\n}",
			expected: `{"key":"value"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := compactNonStringWhitespace(tt.input)
			if result != tt.expected {
				t.Errorf("compactNonStringWhitespace(%v) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}

// Test helper functions
func TestIsLikelyJSONObject(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{"Valid JSON object", `{"key": "value"}`, true},
		{"Valid JSON with spaces", `{ "key": "value" }`, true},
		{"Not JSON - plain string", "plain string", false},
		{"Not JSON - only opening brace", "{incomplete", false},
		{"Empty string", "", false},
		{"JSON array", `["item1", "item2"]`, false}, // Arrays should return false for this specific function
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isLikelyJSONObject(tt.input)
			if result != tt.expected {
				t.Errorf("isLikelyJSONObject(%v) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}

// Integration tests for NewReplacer with variants
func TestNewReplacer_WithVariants(t *testing.T) {
	tests := []struct {
		name     string
		secrets  []string
		input    string
		expected string
	}{
		{
			name:     "JSON variant masking - pretty to compact",
			secrets:  []string{`{"token": "secret123"}`},                // Pretty format secret
			input:    `curl -d '{"token":"secret123"}' api.example.com`, // Compact format in command
			expected: `curl -d '**************' api.example.com`,
		},
		{
			name:     "Shell variable masking",
			secrets:  []string{"export TOKEN=$SECRET_VALUE"}, // Original format
			input:    "export TOKEN=actual_secret_here",      // Expanded format
			expected: "**************actual_secret_here",     // Should mask the variable part
		},
		{
			name:     "Quote stripping masking",
			secrets:  []string{`"secret123"`}, // With quotes
			input:    "password is secret123", // Without quotes
			expected: "password is **************",
		},
		{
			name:     "URL encoding masking",
			secrets:  []string{"hello world"},                  // Original
			input:    "curl 'https://api.com?msg=hello+world'", // URL encoded
			expected: "curl 'https://api.com?msg=**************'",
		},
		{
			name:     "Multiple secret variants",
			secrets:  []string{`{"api_key": "abc123"}`},
			input:    "curl -H 'Content-Type: application/json' -d '{\"api_key\":\"abc123\"}' endpoint",
			expected: "curl -H 'Content-Type: application/json' -d '**************' endpoint",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sw := &nopWriter{}
			// Enable advanced secret masking for these tests
			envs := map[string]string{"CI_ENABLE_EXTRA_CHARACTERS_SECRETS_MASKING": "true"}
			w := NewReplacerWithEnvs(&nopCloser{sw}, tt.secrets, envs)
			_, _ = w.Write([]byte(tt.input))
			w.Close()

			if len(sw.data) == 0 {
				t.Fatal("No data written")
			}

			got := sw.data[0]
			if got != tt.expected {
				t.Errorf("NewReplacer masking failed.\nInput: %v\nSecrets: %v\nGot: %v\nWant: %v", tt.input, tt.secrets, got, tt.expected)
			}
		})
	}
}

// Test that basic masking still works without the feature flag
func TestNewReplacer_BasicMasking(t *testing.T) {
	tests := []struct {
		name     string
		secrets  []string
		input    string
		expected string
	}{
		{
			name:     "Basic secret masking",
			secrets:  []string{"secret123"},
			input:    "password is secret123",
			expected: "password is **************",
		},
		{
			name:     "JSON secret should NOT be masked without flag",
			secrets:  []string{`{"token": "secret123"}`},
			input:    "curl -d '{\"token\":\"secret123\"}' api.example.com",
			expected: "curl -d '{\"token\":\"secret123\"}' api.example.com", // Should NOT be masked
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sw := &nopWriter{}
			// Use basic NewReplacer without feature flag
			w := NewReplacer(&nopCloser{sw}, tt.secrets)
			_, _ = w.Write([]byte(tt.input))
			w.Close()

			if len(sw.data) == 0 {
				t.Fatal("No data written")
			}

			got := sw.data[0]
			if got != tt.expected {
				t.Errorf("Basic masking failed.\nInput: %v\nSecrets: %v\nGot: %v\nWant: %v", tt.input, tt.secrets, got, tt.expected)
			}
		})
	}
}

type nopCloser struct {
	Writer
}
