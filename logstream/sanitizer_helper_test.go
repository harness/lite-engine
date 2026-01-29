// Copyright 2025 Harness Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package logstream

import (
	"encoding/base64"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
)

// Helper function to load patterns for testing (simulates delegate behavior)
func loadTestPatterns(t *testing.T, patterns []string) {
	t.Helper()

	// Reset patterns for test isolation
	maskingPatterns = []*regexp.Regexp{}
	customPatternsLoaded = false

	if len(patterns) == 0 {
		return
	}

	// Join patterns and encode (same as delegate does)
	content := ""
	for i, pattern := range patterns {
		if i > 0 {
			content += "\n"
		}
		content += pattern
	}

	// Base64 encode
	base64Content := base64.StdEncoding.EncodeToString([]byte(content))

	// Decode and load (simulating what lite-engine does)
	decoded, err := base64.StdEncoding.DecodeString(base64Content)
	assert.NoError(t, err)

	err = LoadCustomPatternsFromString(string(decoded))
	assert.NoError(t, err)
}

func TestLoadCustomPatternsFromString_ValidPatterns(t *testing.T) {
	patterns := []string{
		`ghp_[a-zA-Z0-9]{1,50}`,
		`github_pat_[a-zA-Z0-9_]+`,
		`Bearer\s+[A-Za-z0-9_\-.]+`,
	}

	loadTestPatterns(t, patterns)

	assert.Equal(t, 3, GetMaskingPatternsCount())
}

func TestLoadCustomPatternsFromString_EmptyContent(t *testing.T) {
	maskingPatterns = []*regexp.Regexp{}
	customPatternsLoaded = false

	err := LoadCustomPatternsFromString("")
	assert.NoError(t, err)
	assert.Equal(t, 0, GetMaskingPatternsCount())
}

func TestLoadCustomPatternsFromString_SkipComments(t *testing.T) {
	content := `# This is a comment
ghp_[a-zA-Z0-9]{1,50}
# Another comment
github_pat_[a-zA-Z0-9_]+`

	maskingPatterns = []*regexp.Regexp{}
	customPatternsLoaded = false

	err := LoadCustomPatternsFromString(content)
	assert.NoError(t, err)
	assert.Equal(t, 2, GetMaskingPatternsCount())
}

func TestLoadCustomPatternsFromString_InvalidPattern(t *testing.T) {
	content := `valid_pattern
[invalid(pattern
another_valid`

	maskingPatterns = []*regexp.Regexp{}
	customPatternsLoaded = false

	err := LoadCustomPatternsFromString(content)
	assert.NoError(t, err)
	// Should load 2 valid patterns, skip invalid
	assert.Equal(t, 2, GetMaskingPatternsCount())
}

func TestLoadCustomPatternsFromString_PreventDuplicateLoad(t *testing.T) {
	patterns := []string{`test_pattern`}

	loadTestPatterns(t, patterns)
	assert.Equal(t, 1, GetMaskingPatternsCount())

	// Try loading again - should skip
	err := LoadCustomPatternsFromString("test_pattern")
	assert.NoError(t, err)
	assert.Equal(t, 1, GetMaskingPatternsCount(), "Should not add duplicate patterns")
}

func TestSanitizeTokens_GitHubTokens(t *testing.T) {
	loadTestPatterns(t, []string{`ghp_[a-zA-Z0-9]{1,50}`})

	input := "My GitHub token is ghp_1234567890abcdefghijklmnopqrstuvwxy in this log"
	expected := "My GitHub token is ************** in this log"

	result := SanitizeTokens(input)
	assert.Equal(t, expected, result)
}

func TestSanitizeTokens_GitHubPAT(t *testing.T) {
	loadTestPatterns(t, []string{`github_pat_[a-zA-Z0-9_]+`})

	input := "Using github_pat_11ABCDEFG0123456789_abcdefghijklmnopqrstuvwxyz for authentication"
	expected := "Using ************** for authentication"

	result := SanitizeTokens(input)
	assert.Equal(t, expected, result)
}

func TestSanitizeTokens_GitLabToken(t *testing.T) {
	loadTestPatterns(t, []string{`glpat-[A-Za-z0-9\-_]{20}`})

	input := "GitLab token: glpat-ABCDEFGHIJ1234567890"
	expected := "GitLab token: **************"

	result := SanitizeTokens(input)
	assert.Equal(t, expected, result)
}

func TestSanitizeTokens_SlackWebhook(t *testing.T) {
	loadTestPatterns(t, []string{`T[a-zA-Z0-9_]{8}/B[a-zA-Z0-9_]{8,10}/[a-zA-Z0-9_]{24}`})

	// Using FAKE Slack webhook URL for testing (not a real webhook)
	// Pattern: T[8 chars]/B[8-10 chars]/[24 chars]
	input := "Slack webhook: https://example.com/TXXXXXXXX/BXXXXXXXX/FakeFakeFakeFakeFake1234"
	expected := "Slack webhook: https://example.com/**************"

	result := SanitizeTokens(input)
	assert.Equal(t, expected, result)
}

func TestSanitizeTokens_BearerToken(t *testing.T) {
	loadTestPatterns(t, []string{`Bearer\s+[A-Za-z0-9_\-.]+`})

	// gitleaks:allow - This is a test fixture, not a real secret (example JWT from jwt.io)
	input := "Authorization: Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dozjgNryP4J3jVmNHl0w5N_XgL0n3I9PlFUP0THsR8U"
	expected := "Authorization: **************"

	result := SanitizeTokens(input)
	assert.Equal(t, expected, result)
}

func TestSanitizeTokens_BasicAuth(t *testing.T) {
	loadTestPatterns(t, []string{`Basic\s+[A-Za-z0-9_\-.\+/=]+`})

	input := "Authorization: Basic dXNlcm5hbWU6cGFzc3dvcmQ="
	expected := "Authorization: **************"

	result := SanitizeTokens(input)
	assert.Equal(t, expected, result)
}

func TestSanitizeTokens_JWT(t *testing.T) {
	loadTestPatterns(t, []string{`[\w-]+\.[\w-]+\.[\w-]+`})

	// gitleaks:allow - This is a test fixture, not a real secret (example JWT from jwt.io)
	input := "Token: eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIiwibmFtZSI6IkpvaG4gRG9lIiwiaWF0IjoxNTE2MjM5MDIyfQ.SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJV_adQssw5c"
	result := SanitizeTokens(input)

	// JWT should be masked
	assert.Contains(t, result, "**************")
	assert.NotContains(t, result, "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9")
}

func TestSanitizeTokens_CreditCardVisa(t *testing.T) {
	loadTestPatterns(t, []string{`\b4\d{12}(?:\d{3})?\b`})

	input := "Credit card: 4111111111111111 for payment"
	expected := "Credit card: ************** for payment"

	result := SanitizeTokens(input)
	assert.Equal(t, expected, result)
}

func TestSanitizeTokens_CreditCardMastercard(t *testing.T) {
	loadTestPatterns(t, []string{`\b5[1-5]\d{14}\b`})

	input := "Card number: 5500000000000004"
	expected := "Card number: **************"

	result := SanitizeTokens(input)
	assert.Equal(t, expected, result)
}

func TestSanitizeTokens_CreditCardAmex(t *testing.T) {
	loadTestPatterns(t, []string{`\b3[47]\d{13}\b`})

	input := "Amex: 340000000000009"
	expected := "Amex: **************"

	result := SanitizeTokens(input)
	assert.Equal(t, expected, result)
}

func TestSanitizeTokens_SSN(t *testing.T) {
	loadTestPatterns(t, []string{`\b\d{3}-\d{2}-\d{4}\b`})

	input := "SSN: 123-45-6789 on file"
	expected := "SSN: ************** on file"

	result := SanitizeTokens(input)
	assert.Equal(t, expected, result)
}

func TestSanitizeTokens_MultiplePatterns(t *testing.T) {
	loadTestPatterns(t, []string{
		`ghp_[a-zA-Z0-9]{1,50}`,
		`\b\d{3}-\d{2}-\d{4}\b`,
	})

	input := "GitHub token ghp_abcdefg123456789 and SSN 123-45-6789 leaked"
	result := SanitizeTokens(input)

	// Both should be masked
	assert.NotContains(t, result, "ghp_abcdefg123456789")
	assert.NotContains(t, result, "123-45-6789")
	assert.Contains(t, result, "**************")
}

func TestSanitizeTokens_EmptyString(t *testing.T) {
	loadTestPatterns(t, []string{})

	input := ""
	result := SanitizeTokens(input)
	assert.Equal(t, "", result)
}

func TestSanitizeTokens_NoSecrets(t *testing.T) {
	loadTestPatterns(t, []string{`ghp_[a-zA-Z0-9]{1,50}`})

	input := "This is a normal log line with no secrets"
	result := SanitizeTokens(input)
	assert.Equal(t, input, result)
}

func TestSanitizeTokens_NoPatternsLoaded(t *testing.T) {
	// Reset to empty
	maskingPatterns = []*regexp.Regexp{}
	customPatternsLoaded = false

	input := "GitHub token ghp_1234567890abcdefghijklmnopqrstuvwxy"
	result := SanitizeTokens(input)

	// Without patterns, nothing should be masked
	assert.Equal(t, input, result)
}

func TestSanitizeTokens_CustomXMLPatterns(t *testing.T) {
	loadTestPatterns(t, []string{
		`<accountCode>.*?</accountCode>`,
		`<b:taxIdNumber>.*?</b:taxIdNumber>`,
	})

	input := `<response>
		<accountCode>12345ABC</accountCode>
		<b:taxIdNumber>123-45-6789</b:taxIdNumber>
		<balance>1000.00</balance>
	</response>`

	result := SanitizeTokens(input)

	// Account code and tax ID should be masked
	assert.NotContains(t, result, "12345ABC")
	assert.NotContains(t, result, "123-45-6789")
	assert.Contains(t, result, "**************")

	// Balance should NOT be masked (not a sensitive pattern)
	assert.Contains(t, result, "1000.00")
}

func TestSanitizeTokens_LongString(t *testing.T) {
	loadTestPatterns(t, []string{`ghp_[a-zA-Z0-9]{1,50}`})

	// Test with very long input (performance check)
	longInput := ""
	for i := 0; i < 1000; i++ {
		longInput += "This is a normal log line with no secrets. "
	}

	result := SanitizeTokens(longInput)

	// Should complete without panic
	assert.NotEmpty(t, result)
}

func TestSanitizeTokens_SpecialCharacters(t *testing.T) {
	loadTestPatterns(t, []string{})

	input := "Special chars: \n\t\r !@#$%^&*()_+-=[]{}|;':\",./<>?"
	result := SanitizeTokens(input)

	// Should handle special characters without error
	assert.NotEmpty(t, result)
}

func TestSanitizeTokens_Unicode(t *testing.T) {
	loadTestPatterns(t, []string{})

	input := "Unicode: 你好世界 🚀 Emoji test"
	result := SanitizeTokens(input)

	// Should handle unicode without error
	assert.Equal(t, input, result) // No secrets, should be unchanged
}

func TestSanitizeTokens_BoundaryConditions(t *testing.T) {
	loadTestPatterns(t, []string{
		`\b\d{3}-\d{2}-\d{4}\b`,
		`\b4\d{12}(?:\d{3})?\b`,
		`\b5[1-5]\d{14}\b`,
	})

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "SSN at start of line",
			input:    "123-45-6789 is the SSN",
			expected: "************** is the SSN",
		},
		{
			name:     "SSN at end of line",
			input:    "SSN is 123-45-6789",
			expected: "SSN is **************",
		},
		{
			name:     "Credit card with no surrounding text",
			input:    "4111111111111111",
			expected: "**************",
		},
		{
			name:     "Multiple credit cards",
			input:    "Cards: 4111111111111111 and 5500000000000004",
			expected: "Cards: ************** and **************",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SanitizeTokens(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestSanitizeTokens_CaseSensitivity(t *testing.T) {
	loadTestPatterns(t, []string{
		`ghp_[a-zA-Z0-9]{1,50}`,
		`glpat-[A-Za-z0-9\-_]{20}`,
	})

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "GitHub token lowercase",
			input:    "ghp_abcdefghij1234567890",
			expected: "**************",
		},
		{
			name:     "GitHub token with different case should NOT match",
			input:    "GHP_ABCDEFGHIJ1234567890",
			expected: "GHP_ABCDEFGHIJ1234567890", // Pattern is case-sensitive
		},
		{
			name:     "GitLab token",
			input:    "glpat-ABCDEFGHIJ1234567890",
			expected: "**************",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SanitizeTokens(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestGetMaskingPatternsCount(t *testing.T) {
	loadTestPatterns(t, []string{
		`pattern1`,
		`pattern2`,
		`pattern3`,
	})

	count := GetMaskingPatternsCount()
	assert.Equal(t, 3, count)
}

// Performance test: ensure regex patterns don't cause significant slowdown
func BenchmarkSanitizeTokens_NoSecrets(b *testing.B) {
	// Load typical patterns
	loadTestPatterns(&testing.T{}, []string{
		`ghp_[a-zA-Z0-9]{1,50}`,
		`github_pat_[a-zA-Z0-9_]+`,
		`Bearer\s+[A-Za-z0-9_\-.]+`,
		`\b4\d{12}(?:\d{3})?\b`,
	})

	input := "This is a normal log line with no secrets or tokens to mask"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = SanitizeTokens(input)
	}
}

func BenchmarkSanitizeTokens_WithSecrets(b *testing.B) {
	// Load typical patterns
	loadTestPatterns(&testing.T{}, []string{
		`ghp_[a-zA-Z0-9]{1,50}`,
		`\b4\d{12}(?:\d{3})?\b`,
	})

	input := "GitHub token ghp_abcdefghijklmnopqrstuvwxyz123456 and credit card 4111111111111111"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = SanitizeTokens(input)
	}
}
