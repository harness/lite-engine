// Copyright 2025 Harness Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package logstream

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSanitizeTokens_GitHubTokens(t *testing.T) {
	input := "My GitHub token is ghp_1234567890abcdefghijklmnopqrstuvwxy in this log"
	expected := "My GitHub token is ************** in this log"

	result := SanitizeTokens(input)
	assert.Equal(t, expected, result)
}

func TestSanitizeTokens_GitHubPAT(t *testing.T) {
	input := "Using github_pat_11ABCDEFG0123456789_abcdefghijklmnopqrstuvwxyz for authentication"
	expected := "Using ************** for authentication"

	result := SanitizeTokens(input)
	assert.Equal(t, expected, result)
}

func TestSanitizeTokens_GitLabToken(t *testing.T) {
	input := "GitLab token: glpat-ABCDEFGHIJ1234567890"
	expected := "GitLab token: **************"

	result := SanitizeTokens(input)
	assert.Equal(t, expected, result)
}

func TestSanitizeTokens_SlackWebhook(t *testing.T) {
	// Using FAKE Slack webhook URL for testing (not a real webhook)
	// Pattern: T[8 chars]/B[8-10 chars]/[24 chars]
	input := "Slack webhook: https://example.com/TXXXXXXXX/BXXXXXXXX/FakeFakeFakeFakeFake1234"
	expected := "Slack webhook: https://example.com/**************"

	result := SanitizeTokens(input)
	assert.Equal(t, expected, result)
}

func TestSanitizeTokens_BearerToken(t *testing.T) {
	// gitleaks:allow - This is a test fixture, not a real secret (example JWT from jwt.io)
	input := "Authorization: Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dozjgNryP4J3jVmNHl0w5N_XgL0n3I9PlFUP0THsR8U"
	expected := "Authorization: **************"

	result := SanitizeTokens(input)
	assert.Equal(t, expected, result)
}

func TestSanitizeTokens_BasicAuth(t *testing.T) {
	input := "Authorization: Basic dXNlcm5hbWU6cGFzc3dvcmQ="
	expected := "Authorization: **************"

	result := SanitizeTokens(input)
	assert.Equal(t, expected, result)
}

func TestSanitizeTokens_ValidJWT(t *testing.T) {
	// gitleaks:allow - This is a test fixture, not a real secret (example JWT from jwt.io)
	input := "Token: eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIiwibmFtZSI6IkpvaG4gRG9lIiwiaWF0IjoxNTE2MjM5MDIyfQ.SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJV_adQssw5c"
	result := SanitizeTokens(input)

	// JWT should be masked
	assert.Contains(t, result, "**************")
	assert.NotContains(t, result, "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9")
}

func TestSanitizeTokens_InvalidJWT_NotMasked(t *testing.T) {
	// This looks like JWT format but is invalid
	input := "Not a token: abc.def.ghi just some text"
	result := SanitizeTokens(input)

	// Should NOT be masked because it's not a valid JWT
	assert.Equal(t, input, result)
}

func TestSanitizeTokens_CreditCardVisa(t *testing.T) {
	input := "Credit card: 4111111111111111 for payment"
	expected := "Credit card: ************** for payment"

	result := SanitizeTokens(input)
	assert.Equal(t, expected, result)
}

func TestSanitizeTokens_CreditCardMastercard(t *testing.T) {
	input := "Card number: 5500000000000004"
	expected := "Card number: **************"

	result := SanitizeTokens(input)
	assert.Equal(t, expected, result)
}

func TestSanitizeTokens_CreditCardAmex(t *testing.T) {
	input := "Amex: 340000000000009"
	expected := "Amex: **************"

	result := SanitizeTokens(input)
	assert.Equal(t, expected, result)
}

func TestSanitizeTokens_SSN(t *testing.T) {
	input := "SSN: 123-45-6789 on file"
	expected := "SSN: ************** on file"

	result := SanitizeTokens(input)
	assert.Equal(t, expected, result)
}

func TestSanitizeTokens_MultiplePatterns(t *testing.T) {
	input := "GitHub token ghp_abcdefg123456789 and SSN 123-45-6789 leaked"
	result := SanitizeTokens(input)

	// Both should be masked
	assert.NotContains(t, result, "ghp_abcdefg123456789")
	assert.NotContains(t, result, "123-45-6789")
	assert.Contains(t, result, "**************")
}

func TestSanitizeTokens_EmptyString(t *testing.T) {
	input := ""
	result := SanitizeTokens(input)
	assert.Equal(t, "", result)
}

func TestSanitizeTokens_NoSecrets(t *testing.T) {
	input := "This is a normal log line with no secrets"
	result := SanitizeTokens(input)
	assert.Equal(t, input, result)
}

func TestLoadPatternsFromFile_ValidFile(t *testing.T) {
	// Create temp file with patterns
	tmpDir := t.TempDir()
	patternFile := filepath.Join(tmpDir, "patterns.txt")

	content := `<accountCode>.*?</accountCode>
<b:taxIdNumber>.*?</b:taxIdNumber>
\[SqlReader\]: Key: .*`

	err := os.WriteFile(patternFile, []byte(content), 0600)
	assert.NoError(t, err)

	// Load patterns
	patterns := loadPatternsFromFile(patternFile)

	assert.Len(t, patterns, 3)
	assert.Equal(t, "<accountCode>.*?</accountCode>", patterns[0].String())
	assert.Equal(t, "<b:taxIdNumber>.*?</b:taxIdNumber>", patterns[1].String())
	assert.Equal(t, `\[SqlReader\]: Key: .*`, patterns[2].String())
}

func TestLoadPatternsFromFile_EmptyFile(t *testing.T) {
	tmpDir := t.TempDir()
	patternFile := filepath.Join(tmpDir, "empty.txt")

	err := os.WriteFile(patternFile, []byte(""), 0600)
	assert.NoError(t, err)

	patterns := loadPatternsFromFile(patternFile)
	assert.Len(t, patterns, 0)
}

func TestLoadPatternsFromFile_FileNotExist(t *testing.T) {
	patterns := loadPatternsFromFile("/nonexistent/file.txt")
	assert.Len(t, patterns, 0)
}

func TestLoadPatternsFromFile_InvalidPattern(t *testing.T) {
	tmpDir := t.TempDir()
	patternFile := filepath.Join(tmpDir, "invalid.txt")

	// Invalid regex (unclosed bracket)
	content := `valid_pattern
[invalid(pattern
another_valid`

	err := os.WriteFile(patternFile, []byte(content), 0600)
	assert.NoError(t, err)

	patterns := loadPatternsFromFile(patternFile)

	// Should skip invalid pattern and only load valid ones
	assert.Len(t, patterns, 2)
}

func TestSanitizeTokens_CustomPatterns(t *testing.T) {
	// Create temp file with custom patterns
	tmpDir := t.TempDir()
	patternFile := filepath.Join(tmpDir, "custom.txt")

	content := `<CreditCard>.*?</CreditCard>
<SSN>.*?</SSN>
ACCT-\d{8,12}`

	err := os.WriteFile(patternFile, []byte(content), 0600)
	assert.NoError(t, err)

	// Load patterns
	customPatterns := loadPatternsFromFile(patternFile)

	// Manually add to maskingPatterns for this test
	originalLen := len(maskingPatterns)
	maskingPatterns = append(maskingPatterns, customPatterns...)
	defer func() {
		// Restore original patterns after test
		maskingPatterns = maskingPatterns[:originalLen]
	}()

	input := "Data: <CreditCard>4111-1111-1111-1111</CreditCard> and <SSN>123-45-6789</SSN> and ACCT-123456789"
	result := SanitizeTokens(input)

	// The regex ACCT-\d{8,12} matches just the digits after ACCT-, so result will be "ACCT-**************"
	assert.Equal(t, "Data: ************** and ************** and ACCT-**************", result)
}

func TestSanitizeTokens_XMLPattern(t *testing.T) {
	// Add custom XML pattern for this test
	err := AddCustomPattern(`<accountCode>.*?</accountCode>`)
	assert.NoError(t, err)

	input := "Response: <accountCode>ACCT123456</accountCode>"
	result := SanitizeTokens(input)

	assert.Equal(t, "Response: **************", result)
}

func TestAddCustomPattern_InvalidRegex(t *testing.T) {
	err := AddCustomPattern("[invalid(")
	assert.Error(t, err)
}

func TestGetMaskingPatternsCount(t *testing.T) {
	count := GetMaskingPatternsCount()
	// Should have at least the built-in patterns (12 financial + auth patterns)
	assert.GreaterOrEqual(t, count, 12)
}

func TestIsValidJWT_Valid(t *testing.T) {
	// gitleaks:allow - This is a test fixture, not a real secret (example JWT from jwt.io)
	token := "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIiwibmFtZSI6IkpvaG4gRG9lIiwiaWF0IjoxNTE2MjM5MDIyfQ.SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJV_adQssw5c"
	assert.True(t, isValidJWT(token))
}

func TestIsValidJWT_Invalid(t *testing.T) {
	assert.False(t, isValidJWT("not.a.token"))
	assert.False(t, isValidJWT("abc"))
	assert.False(t, isValidJWT(""))
}

// Integration test with Capital One use case (XML patterns)
func TestSanitizeTokens_CapitalOneXMLPatterns(t *testing.T) {
	// Add Capital One specific patterns
	err := AddCustomPattern(`<accountCode>.*?</accountCode>`)
	assert.NoError(t, err)
	err = AddCustomPattern(`<b:taxIdNumber>.*?</b:taxIdNumber>`)
	assert.NoError(t, err)

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

// Performance test: ensure regex patterns don't cause significant slowdown
func BenchmarkSanitizeTokens_NoSecrets(b *testing.B) {
	input := "This is a normal log line with no secrets or tokens to mask"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = SanitizeTokens(input)
	}
}

func BenchmarkSanitizeTokens_WithSecrets(b *testing.B) {
	input := "GitHub token ghp_abcdefghijklmnopqrstuvwxyz123456 and credit card 4111111111111111"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = SanitizeTokens(input)
	}
}

// Additional edge case tests for better coverage

func TestLoadPatternsFromFile_FileWithBlankLines(t *testing.T) {
	tmpDir := t.TempDir()
	patternFile := filepath.Join(tmpDir, "patterns.txt")

	// File with blank lines and whitespace
	content := `
pattern1

pattern2


pattern3
   `

	err := os.WriteFile(patternFile, []byte(content), 0600)
	assert.NoError(t, err)

	patterns := loadPatternsFromFile(patternFile)

	// Should only load non-empty patterns
	assert.Len(t, patterns, 3)
}

func TestLoadPatternsFromFile_MixedValidInvalid(t *testing.T) {
	tmpDir := t.TempDir()
	patternFile := filepath.Join(tmpDir, "patterns.txt")

	// Mix of valid and invalid patterns
	content := `validPattern1
[invalid(pattern
validPattern2
**invalid
validPattern3`

	err := os.WriteFile(patternFile, []byte(content), 0600)
	assert.NoError(t, err)

	patterns := loadPatternsFromFile(patternFile)

	// Should skip invalid patterns
	assert.Len(t, patterns, 3, "Should load 3 valid patterns")
}

func TestLoadPatternsFromFile_UnreadableFile(t *testing.T) {
	tmpDir := t.TempDir()
	patternFile := filepath.Join(tmpDir, "patterns.txt")

	// Create file
	err := os.WriteFile(patternFile, []byte("pattern1"), 0600)
	assert.NoError(t, err)

	// Make file unreadable (permissions 000)
	err = os.Chmod(patternFile, 0000)
	if err != nil {
		t.Skip("Cannot change file permissions on this system")
	}
	defer os.Chmod(patternFile, 0600) // Cleanup

	// Check if we're running as root (root can read files with 0000 permissions)
	if os.Geteuid() == 0 {
		t.Skip("Skipping unreadable file test when running as root")
	}

	patterns := loadPatternsFromFile(patternFile)

	// Should return empty list on permission error
	assert.Len(t, patterns, 0)
}

func TestSanitizeTokens_LongString(t *testing.T) {
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
	input := "Special chars: \n\t\r !@#$%^&*()_+-=[]{}|;':\",./<>?"
	result := SanitizeTokens(input)

	// Should handle special characters without error
	assert.NotEmpty(t, result)
}

func TestSanitizeTokens_Unicode(t *testing.T) {
	input := "Unicode: 你好世界 🚀 Emoji test"
	result := SanitizeTokens(input)

	// Should handle unicode without error
	assert.Equal(t, input, result) // No secrets, should be unchanged
}

func TestSanitizeTokens_BoundaryConditions(t *testing.T) {
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

func TestAddCustomPattern_DuplicatePattern(t *testing.T) {
	// Get initial count
	initialCount := GetMaskingPatternsCount()

	// Add same pattern twice
	err1 := AddCustomPattern(`DUPLICATE-\d{8}`)
	err2 := AddCustomPattern(`DUPLICATE-\d{8}`)

	assert.NoError(t, err1)
	assert.NoError(t, err2)

	// Both should be added (no deduplication logic)
	assert.Equal(t, initialCount+2, GetMaskingPatternsCount())
}

func TestSanitizeTokens_CaseSensitivity(t *testing.T) {
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

func TestIsValidJWT_EdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		token    string
		expected bool
	}{
		{
			name: "valid JWT",
			// gitleaks:allow - This is a test fixture, not a real secret (example JWT from jwt.io)
			token:    "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dozjgNryP4J3jVmNHl0w5N_XgL0n3I9PlFUP0THsR8U",
			expected: true,
		},
		{
			name:     "empty string",
			token:    "",
			expected: false,
		},
		{
			name:     "single dot",
			token:    ".",
			expected: false,
		},
		{
			name:     "two dots only",
			token:    "..",
			expected: false,
		},
		{
			name:     "looks like JWT but invalid",
			token:    "header.payload.signature",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isValidJWT(tt.token)
			assert.Equal(t, tt.expected, result)
		})
	}
}
