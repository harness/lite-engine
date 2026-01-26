// Copyright 2025 Harness Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package logstream

import (
	"bufio"
	"os"
	"regexp"
	"strings"

	"github.com/golang-jwt/jwt/v4"
	"github.com/sirupsen/logrus"
)

const (
	// secretMask is the string used to replace secrets in regex sanitization
	secretMask = "**************"

	// Regex patterns for various token types
	jwtRegex        = `[\w-]+\.[\w-]+\.[\w-]+`
	githubTokens    = `ghp_[a-zA-Z0-9]{1,50}`    // #nosec G101 -- This is a regex pattern, not a credential
	githubNewTokens = `github_pat_[a-zA-Z0-9_]+` // #nosec G101 -- This is a regex pattern, not a credential
	slackWebhook    = `T[a-zA-Z0-9_]{8}/B[a-zA-Z0-9_]{8,10}/[a-zA-Z0-9_]{24}`
	bearerTokens    = `Bearer\s+[A-Za-z0-9_\-.]+`    // #nosec G101 -- This is a regex pattern, not a credential
	basicTokens     = `Basic\s+[A-Za-z0-9_\-.\+/=]+` // #nosec G101 -- This is a regex pattern, not a credential
	gitlabToken     = `glpat-[A-Za-z0-9\-_]{20}`     // #nosec G101 -- This is a regex pattern, not a credential

	// Financial patterns (PCI DSS compliance)
	creditCardVisa       = `\b4\d{12}(?:\d{3})?\b`     // #nosec G101 -- This is a regex pattern, not a credential
	creditCardMastercard = `\b5[1-5]\d{14}\b`          // #nosec G101 -- This is a regex pattern, not a credential
	creditCardAmex       = `\b3[47]\d{13}\b`           // #nosec G101 -- This is a regex pattern, not a credential
	creditCardDiscover   = `\b6(?:011|5\d{2})\d{12}\b` // #nosec G101 -- This is a regex pattern, not a credential
	ssnPattern           = `\b\d{3}-\d{2}-\d{4}\b`
	bankAccountPattern   = `\b\d{8,17}\b`

	// Default path for custom patterns file
	sanitizePatternsFile = "/etc/lite-engine/sanitize-patterns.txt"
)

var (
	// maskingPatterns holds all compiled regex patterns
	maskingPatterns []*regexp.Regexp

	// jwtPattern is used separately for JWT validation
	jwtPattern *regexp.Regexp
)

// init loads all built-in patterns and custom patterns from file
//
//nolint:gochecknoinits // Init required to compile regex patterns at startup for performance
func init() {
	// Compile JWT pattern
	jwtPattern = regexp.MustCompile(jwtRegex)

	// Compile built-in patterns
	maskingPatterns = []*regexp.Regexp{
		regexp.MustCompile(githubTokens),
		regexp.MustCompile(githubNewTokens),
		regexp.MustCompile(slackWebhook),
		regexp.MustCompile(bearerTokens),
		regexp.MustCompile(basicTokens),
		regexp.MustCompile(gitlabToken),
		regexp.MustCompile(creditCardVisa),
		regexp.MustCompile(creditCardMastercard),
		regexp.MustCompile(creditCardAmex),
		regexp.MustCompile(creditCardDiscover),
		regexp.MustCompile(ssnPattern),
		regexp.MustCompile(bankAccountPattern),
	}

	// Load custom patterns from file if it exists
	customPatterns := loadPatternsFromFile(sanitizePatternsFile)
	maskingPatterns = append(maskingPatterns, customPatterns...)
}

// SanitizeTokens masks tokens and sensitive data using regex patterns
// This is the main entry point, equivalent to LogSanitizerHelper.sanitizeTokens()
func SanitizeTokens(message string) string {
	if message == "" {
		return message
	}

	// 1. JWT validation and masking (avoid false positives)
	message = sanitizeJWTs(message)

	// 2. Handle Bearer/Basic tokens specially (preserve prefix)
	bearerPattern := regexp.MustCompile(bearerTokens)
	message = bearerPattern.ReplaceAllStringFunc(message, func(match string) string {
		// Replace "Bearer <token>" with "Bearer **************"
		return "Bearer " + secretMask
	})

	basicPattern := regexp.MustCompile(basicTokens)
	message = basicPattern.ReplaceAllStringFunc(message, func(match string) string {
		// Replace "Basic <token>" with "Basic **************"
		return "Basic " + secretMask
	})

	// 3. Apply all other regex patterns (exclude Bearer/Basic which we handled above)
	for i, pattern := range maskingPatterns {
		// Skip Bearer and Basic patterns (indices 3 and 4)
		if i == 3 || i == 4 {
			continue
		}
		message = pattern.ReplaceAllString(message, secretMask)
	}

	return message
}

// sanitizeJWTs validates JWT tokens before masking to avoid false positives
// Equivalent to the JWT validation logic in LogSanitizerHelper
func sanitizeJWTs(message string) string {
	matches := jwtPattern.FindAllString(message, -1)

	for _, match := range matches {
		// Try to parse as JWT to avoid false positives
		if isValidJWT(match) {
			message = strings.ReplaceAll(message, match, secretMask)
		}
	}

	return message
}

// isValidJWT checks if a string is a valid JWT token
func isValidJWT(tokenString string) bool {
	// Parse without validation (just structural check)
	parser := jwt.Parser{
		SkipClaimsValidation: true,
	}

	_, _, err := parser.ParseUnverified(tokenString, jwt.MapClaims{})
	return err == nil
}

// loadPatternsFromFile reads regex patterns from a file (one per line)
// Equivalent to LogSanitizerHelper.readPatternsFromFile()
func loadPatternsFromFile(filename string) []*regexp.Regexp {
	file, err := os.Open(filename)
	if err != nil {
		// File doesn't exist or can't be read - this is expected in many cases
		if !os.IsNotExist(err) {
			logrus.WithError(err).WithField("file", filename).Debug("could not open sanitize patterns file")
		}
		return []*regexp.Regexp{}
	}
	defer file.Close()

	var patterns []*regexp.Regexp
	scanner := bufio.NewScanner(file)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		// Skip empty lines
		if line == "" {
			continue
		}

		// Compile the pattern
		pattern, err := regexp.Compile(line)
		if err != nil {
			logrus.WithError(err).WithField("pattern", line).Warn("invalid regex pattern in sanitize file, skipping")
			continue
		}

		patterns = append(patterns, pattern)
	}

	if err := scanner.Err(); err != nil {
		logrus.WithError(err).WithField("file", filename).Error("error reading sanitize patterns file")
		return []*regexp.Regexp{}
	}

	logrus.WithField("file", filename).WithField("patterns_count", len(patterns)).Info("loaded custom sanitize patterns")
	return patterns
}

// AddCustomPattern dynamically adds a regex pattern at runtime
// This can be used for programmatic pattern addition
func AddCustomPattern(pattern string) error {
	compiled, err := regexp.Compile(pattern)
	if err != nil {
		return err
	}
	maskingPatterns = append(maskingPatterns, compiled)
	return nil
}

// GetMaskingPatternsCount returns the number of active masking patterns
// Useful for testing and diagnostics
func GetMaskingPatternsCount() int {
	return len(maskingPatterns)
}
