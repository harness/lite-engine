// Copyright 2022 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package logstream

import (
	"net/url"
	"regexp"
	"strings"
)

const (
	maskedStr = "**************"
)

// replacer wraps a stream writer with a replacer
type replacer struct {
	w Writer
	r *strings.Replacer
}

// Helper function to check if a string contains any shell special characters
func containsShellSpecialChars(s string) bool {
	specialChars := []string{"$", "\\", "`", "!", "*", "&", "|", ";", "<", ">", "(", ")", "[", "]", "{", "}", "~"}
	for _, char := range specialChars {
		if strings.Contains(s, char) {
			return true
		}
	}
	return false
}

const (
	minSecretLength = 2 // Minimum length for a secret to be considered for masking
)

// Helper function to check if a string is likely a JSON object
func isLikelyJSONObject(s string) bool {
	s = strings.TrimSpace(s)
	return len(s) > 4 &&
		strings.HasPrefix(s, "{") &&
		strings.HasSuffix(s, "}") &&
		strings.Contains(s, `":`) // JSON key-value separator
}

// Helper function to generate variants of a string that may appear in shell output
func createSecretVariants(original string) []string { //nolint:funlen
	// Include the original string
	variants := []string{original}

	// Skip further processing for short strings
	if len(original) <= minSecretLength {
		return variants
	}

	// Used to track unique variants
	uniq := make(map[string]bool)
	uniq[original] = true

	// 1. Handle double quote stripping
	if strings.Contains(original, "\"") {
		doubleQuoteStripped := strings.Replace(original, "\"", "", -1)
		if !uniq[doubleQuoteStripped] && len(doubleQuoteStripped) > minSecretLength {
			variants = append(variants, doubleQuoteStripped)
			uniq[doubleQuoteStripped] = true
		}
	}

	// 2. Handle single quote stripping
	if strings.Contains(original, "'") {
		singleQuoteStripped := strings.Replace(original, "'", "", -1)
		if !uniq[singleQuoteStripped] && len(singleQuoteStripped) > minSecretLength {
			variants = append(variants, singleQuoteStripped)
			uniq[singleQuoteStripped] = true
		}
	}

	// 3. Handle escaped quote stripping
	if strings.Contains(original, "\\\"") {
		escapedQuoteStripped := strings.Replace(original, "\\\"", "", -1)
		if !uniq[escapedQuoteStripped] && len(escapedQuoteStripped) > minSecretLength {
			variants = append(variants, escapedQuoteStripped)
			uniq[escapedQuoteStripped] = true
		}
	}

	// 4. Special JSON handling
	if isLikelyJSONObject(original) {
		// Handle removal of quotes around JSON keys and values
		noQuotesPattern := regexp.MustCompile(`\"([^\"]+)\"\\s*:`)
		noKeyQuotes := noQuotesPattern.ReplaceAllString(original, "$1:")

		valuePattern := regexp.MustCompile(`:\\s*\"([^\"]*)\"`)
		noValueQuotes := valuePattern.ReplaceAllString(noKeyQuotes, ":$1")

		if !uniq[noValueQuotes] && len(noValueQuotes) > minSecretLength {
			variants = append(variants, noValueQuotes)
			uniq[noValueQuotes] = true
		}

		// Handle whitespace compaction (JSON to compact)
		compacted := compactNonStringWhitespace(original)
		if !uniq[compacted] && len(compacted) > minSecretLength && compacted != original {
			variants = append(variants, compacted)
			uniq[compacted] = true
		}
	}

	// 5. Handle shell special character transformations
	if containsShellSpecialChars(original) {
		// Handle $ variable replacement
		if strings.Contains(original, "$") {
			varPattern := regexp.MustCompile(`\$\w+`)
			noVars := varPattern.ReplaceAllString(original, "")
			if !uniq[noVars] && len(noVars) > minSecretLength {
				variants = append(variants, noVars)
				uniq[noVars] = true
			}
		}

		// Handle command substitution removal
		if strings.Contains(original, "`") {
			cmdPattern := regexp.MustCompile("`[^`]+`")
			noCmds := cmdPattern.ReplaceAllString(original, "")
			if !uniq[noCmds] && len(noCmds) > minSecretLength {
				variants = append(variants, noCmds)
				uniq[noCmds] = true
			}
		}
	}

	// 6. URL encoding variants (from original implementation)
	urlEncoded := url.QueryEscape(original)
	if !uniq[urlEncoded] && len(urlEncoded) > minSecretLength {
		variants = append(variants, urlEncoded)
		uniq[urlEncoded] = true
	}

	// Also handle %20 style encoding (spaces as %20 instead of +)
	urlEncoded20 := strings.ReplaceAll(url.QueryEscape(original), "+", "%20")
	if !uniq[urlEncoded20] && len(urlEncoded20) > minSecretLength {
		variants = append(variants, urlEncoded20)
		uniq[urlEncoded20] = true
	}

	// Handle URL path encoding
	urlPathEncoded := url.PathEscape(original)
	if !uniq[urlPathEncoded] && len(urlPathEncoded) > minSecretLength {
		variants = append(variants, urlPathEncoded)
		uniq[urlPathEncoded] = true
	}

	return variants
}

// Helper to compact JSON by removing whitespace outside of strings
func compactNonStringWhitespace(jsonString string) string {
	var result strings.Builder
	inString := false

	for _, c := range jsonString {
		char := string(c)

		// Track if we're inside a string to preserve spaces there
		if char == "\"" {
			// Toggle string state, but handle escaped quotes
			if !inString {
				inString = true
			} else {
				// Check if this quote is escaped
				pos := result.Len() - 1
				if pos >= 0 && string(result.String()[pos]) != "\\" {
					inString = false
				}
			}
			result.WriteString(char)
		} else if !inString && (char == " " || char == "\n" || char == "\t" || char == "\r") {
			// Skip whitespace outside strings
			continue
		} else {
			result.WriteString(char)
		}
	}

	return result.String()
}

// NewReplacer returns a replacer that wraps io.Writer w.
func NewReplacer(w Writer, secrets []string) Writer {
	var oldnew []string
	// Track unique strings to avoid duplicates in the replacer
	uniqPatterns := make(map[string]bool)

	for _, secret := range secrets {
		if secret == "" {
			continue
		}

		for _, part := range strings.Split(secret, "\n") {
			part = strings.TrimSpace(part)

			// avoid masking empty or single character strings.
			if len(part) < minSecretLength { //nolint:gomnd
				continue
			}

			// Get all variants for this part including transformations
			variants := createSecretVariants(part)

			// Add each unique variant to the replacer
			for _, variant := range variants {
				if !uniqPatterns[variant] && len(variant) > minSecretLength {
					uniqPatterns[variant] = true
					oldnew = append(oldnew, variant, maskedStr)
				}
			}
		}
	}

	if len(oldnew) == 0 {
		return w
	}
	return &replacer{
		w: w,
		r: strings.NewReplacer(oldnew...),
	}
}

// Write writes p to the base writer. The method scans for any
// sensitive data in p and masks before writing.
func (r *replacer) Write(p []byte) (n int, err error) {
	_, err = r.w.Write([]byte(r.r.Replace(string(p))))
	return len(p), err
}

// Open opens the base writer.
func (r *replacer) Open() error {
	return r.w.Open()
}

func (r *replacer) Start() {
	r.w.Start()
}

// Close closes the base writer.
func (r *replacer) Close() error {
	return r.w.Close()
}

func (r *replacer) Error() error {
	return r.w.Error()
}
