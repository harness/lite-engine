// Copyright 2025 Harness Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package logstream

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

// Test replacer interface methods (Open, Start, Error) for coverage

type mockWriter struct {
	data      []string
	openErr   error
	closeErr  error
	writeErr  error
	errorVal  error
	opened    bool
	started   bool
}

func (m *mockWriter) Write(p []byte) (n int, err error) {
	if m.writeErr != nil {
		return 0, m.writeErr
	}
	m.data = append(m.data, string(p))
	return len(p), nil
}

func (m *mockWriter) Open() error {
	m.opened = true
	return m.openErr
}

func (m *mockWriter) Start() {
	m.started = true
}

func (m *mockWriter) Close() error {
	return m.closeErr
}

func (m *mockWriter) Error() error {
	return m.errorVal
}

func TestReplacer_Open(t *testing.T) {
	tests := []struct {
		name        string
		openErr     error
		expectError bool
	}{
		{
			name:        "successful open",
			openErr:     nil,
			expectError: false,
		},
		{
			name:        "open error",
			openErr:     errors.New("failed to open stream"),
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockWriter{openErr: tt.openErr}
			replacer := NewReplacer(mock, []string{"secret"})

			err := replacer.Open()

			if tt.expectError {
				assert.Error(t, err)
				assert.Equal(t, tt.openErr, err)
			} else {
				assert.NoError(t, err)
			}
			assert.True(t, mock.opened, "Open should have been called on underlying writer")
		})
	}
}

func TestReplacer_Start(t *testing.T) {
	mock := &mockWriter{}
	replacer := NewReplacer(mock, []string{"secret"})

	replacer.Start()

	assert.True(t, mock.started, "Start should have been called on underlying writer")
}

func TestReplacer_Close(t *testing.T) {
	tests := []struct {
		name        string
		closeErr    error
		expectError bool
	}{
		{
			name:        "successful close",
			closeErr:    nil,
			expectError: false,
		},
		{
			name:        "close error",
			closeErr:    errors.New("failed to close stream"),
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockWriter{closeErr: tt.closeErr}
			replacer := NewReplacer(mock, []string{"secret"})

			err := replacer.Close()

			if tt.expectError {
				assert.Error(t, err)
				assert.Equal(t, tt.closeErr, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestReplacer_Error(t *testing.T) {
	tests := []struct {
		name      string
		errorVal  error
		expectErr bool
	}{
		{
			name:      "no error",
			errorVal:  nil,
			expectErr: false,
		},
		{
			name:      "has error",
			errorVal:  errors.New("stream error"),
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockWriter{errorVal: tt.errorVal}
			replacer := NewReplacer(mock, []string{"secret"})

			err := replacer.Error()

			if tt.expectErr {
				assert.Error(t, err)
				assert.Equal(t, tt.errorVal, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// Integration test: replacer with regex sanitization
func TestReplacer_WithRegexSanitization(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		secrets  []string
		expected string
	}{
		{
			name:     "explicit secret + GitHub token (regex)",
			input:    "Using secret123 and GitHub token ghp_abcdefghij1234567890",
			secrets:  []string{"secret123"},
			expected: "Using ************** and GitHub token **************",
		},
		{
			name:     "credit card in logs (regex only)",
			input:    "Payment processed with card 4111111111111111",
			secrets:  []string{},
			expected: "Payment processed with card **************",
		},
		{
			name:     "JWT token validation",
			input:    "Auth: eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dozjgNryP4J3jVmNHl0w5N_XgL0n3I9PlFUP0THsR8U",
			secrets:  []string{},
			expected: "Auth: **************",
		},
		{
			name:     "multiple patterns",
			input:    "Token: ghp_abc123, Card: 4111111111111111, SSN: 123-45-6789",
			secrets:  []string{},
			expected: "Token: **************, Card: **************, SSN: **************",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockWriter{}
			replacer := NewReplacer(mock, tt.secrets)

			_, err := replacer.Write([]byte(tt.input))
			assert.NoError(t, err)

			assert.Len(t, mock.data, 1)
			assert.Equal(t, tt.expected, mock.data[0])
		})
	}
}

// Test that Write returns correct byte count even after sanitization
func TestReplacer_Write_ByteCount(t *testing.T) {
	mock := &mockWriter{}
	replacer := NewReplacer(mock, []string{"secret123"})

	input := []byte("password: secret123")
	n, err := replacer.Write(input)

	assert.NoError(t, err)
	assert.Equal(t, len(input), n, "Write should return original byte count, not sanitized")
}

// Test Write error propagation
func TestReplacer_Write_Error(t *testing.T) {
	mock := &mockWriter{writeErr: errors.New("write failed")}
	replacer := NewReplacer(mock, []string{"secret"})

	_, err := replacer.Write([]byte("test"))

	assert.Error(t, err)
	assert.Equal(t, "write failed", err.Error())
}

// Test empty secrets list
func TestReplacer_EmptySecrets(t *testing.T) {
	mock := &mockWriter{}
	replacer := NewReplacer(mock, []string{})

	input := "This is a test message"
	_, err := replacer.Write([]byte(input))

	assert.NoError(t, err)
	// Should still apply regex patterns even with no explicit secrets
	assert.Len(t, mock.data, 1)
}

// Test very short secrets (below minSecretLength)
func TestReplacer_ShortSecrets(t *testing.T) {
	mock := &mockWriter{}
	replacer := NewReplacer(mock, []string{"a", "ab"}) // Very short secrets

	input := "Short secrets: a and ab should not be masked"
	_, err := replacer.Write([]byte(input))

	assert.NoError(t, err)
	assert.Len(t, mock.data, 1)
	// "a" (length 1) is below minSecretLength and should NOT be masked
	// "ab" (length 2) equals minSecretLength and SHOULD be masked
	assert.Contains(t, mock.data[0], "a and **************")
	assert.NotContains(t, mock.data[0], "ab")
}

// Test multiline secrets
func TestReplacer_MultilineSecrets(t *testing.T) {
	secret := "line1\nline2\nline3"
	mock := &mockWriter{}
	replacer := NewReplacer(mock, []string{secret})

	input := "Found line2 in logs"
	_, err := replacer.Write([]byte(input))

	assert.NoError(t, err)
	assert.Equal(t, "Found ************** in logs", mock.data[0])
}

// Test nil secrets handling
func TestReplacer_NilSecrets(t *testing.T) {
	mock := &mockWriter{}
	replacer := NewReplacer(mock, nil)

	input := "Test with nil secrets"
	_, err := replacer.Write([]byte(input))

	assert.NoError(t, err)
	assert.Len(t, mock.data, 1)
}

// Test advanced masking mode with envs
func TestReplacer_AdvancedMode_Disabled(t *testing.T) {
	mock := &mockWriter{}
	// Feature flag NOT enabled (or explicitly disabled)
	envs := map[string]string{"CI_ENABLE_EXTRA_CHARACTERS_SECRETS_MASKING": "false"}
	replacer := NewReplacerWithEnvs(mock, []string{`{"token": "secret"}`}, envs)

	input := `{"token":"secret"}` // Compact variant
	_, err := replacer.Write([]byte(input))

	assert.NoError(t, err)
	// Without advanced mode, compact variant should NOT be masked
	assert.Equal(t, input, mock.data[0])
}
