// Copyright 2022 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package external

import (
	"testing"
)

func TestMaskString(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		secrets  []string
		expected string
	}{
		{
			name:     "Basic secret masking",
			input:    "password is secret123",
			secrets:  []string{"secret123"},
			expected: "password is **************",
		},
		{
			name:     "JSON secret masking",
			input:    `{"token": "secret123"}`,
			secrets:  []string{"secret123"},
			expected: `{"token": "**************"}`,
		},
		{
			name:     "No secrets",
			input:    "normal command",
			secrets:  []string{},
			expected: "normal command",
		},
		{
			name:     "JSON variant masking - compact format",
			input:    `curl -d '{"token":"secret123"}' api.example.com`,
			secrets:  []string{`{"token": "secret123"}`},         // Pretty format secret
			expected: `curl -d '**************' api.example.com`, // Should mask compact format
		},
		{
			name:     "Shell variable masking",
			input:    "echo 'token=actual_value'",
			secrets:  []string{"token=$USER_TOKEN"},       // Original format
			expected: "echo '**************actual_value'", // Should mask the variable part
		},
		{
			name:     "Multiple secrets",
			input:    "user=admin password=secret123 token=abc456",
			secrets:  []string{"secret123", "abc456"},
			expected: "user=admin password=************** token=**************",
		},
		{
			name:     "URL encoded secret",
			input:    "curl 'https://api.com?msg=hello+world'",
			secrets:  []string{"hello world"},
			expected: "curl 'https://api.com?msg=**************'",
		},
		{
			name:     "Complex command with secrets",
			input:    `curl -X POST -H "Authorization: Bearer abc123" -d '{"secret": "mysecret"}' https://api.com`,
			secrets:  []string{"abc123", "mysecret"},
			expected: `curl -X POST -H "Authorization: Bearer **************" -d '{"secret": "**************"}' https://api.com`,
		},
		{
			name:     "Multiline secret",
			input:    "Here is a secret:\nline1\nline2\nend",
			secrets:  []string{"line1\nline2"},
			expected: "Here is a secret:\n**************\n**************\nend", // Each line gets masked separately
		},
		{
			name:     "Secret with special characters",
			input:    "password=$p@ssW0rd!",
			secrets:  []string{"$p@ssW0rd!"},
			expected: "password=**************",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := MaskString(tt.input, tt.secrets)
			if result != tt.expected {
				t.Errorf("MaskString() = %v, want %v", result, tt.expected)
			}
		})
	}
}
