// Copyright 2025 Harness Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package logstream

import (
	"regexp"
	"strings"

	"github.com/sirupsen/logrus"
)

const (
	// secretMask is the string used to replace secrets in regex sanitization
	secretMask = "**************"
)

var (
	// maskingPatterns holds all compiled regex patterns from delegate
	// Patterns are loaded during setup via LoadCustomPatternsFromString() from Base64-encoded content
	maskingPatterns []*regexp.Regexp

	// customPatternsLoaded tracks if patterns have been loaded to prevent duplicates
	// This is important for VM hibernation/resume scenarios where lite-engine process persists
	customPatternsLoaded bool
)

// SanitizeTokens masks tokens and sensitive data using regex patterns from delegate
// This is the main entry point, equivalent to LogSanitizerHelper.sanitizeTokens()
// All patterns are loaded from delegate during setup via LoadCustomPatternsFromString()
func SanitizeTokens(message string) string {
	if message == "" {
		return message
	}

	// Apply all regex patterns provided by delegate
	for _, pattern := range maskingPatterns {
		message = pattern.ReplaceAllString(message, secretMask)
	}

	return message
}

// LoadCustomPatternsFromString loads all patterns from delegate-provided string content (one pattern per line)
// This is the ONLY way patterns are loaded into lite-engine - all patterns come from delegate
// (includes both delegate's built-in patterns and any custom patterns configured by user)
// Handles VM hibernation/resume: prevents duplicate loading if patterns already loaded in this process
func LoadCustomPatternsFromString(content string) error {
	if content == "" {
		logrus.Debug("no sanitize patterns provided from delegate")
		return nil
	}

	// Check if patterns were already loaded in this lite-engine process
	// This handles VM hibernation/resume where the process stays alive across multiple builds
	if customPatternsLoaded {
		logrus.WithField("total_patterns", len(maskingPatterns)).
			Debug("patterns already loaded in this process, skipping reload")
		return nil
	}

	lines := strings.Split(content, "\n")
	patternsAdded := 0

	for _, line := range lines {
		line = strings.TrimSpace(line)

		// Skip empty lines and comments
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Compile the pattern
		pattern, err := regexp.Compile(line)
		if err != nil {
			logrus.WithError(err).WithField("pattern", line).Warn("invalid regex pattern from delegate, skipping")
			continue
		}

		maskingPatterns = append(maskingPatterns, pattern)
		patternsAdded++
	}

	if patternsAdded > 0 {
		customPatternsLoaded = true // Mark as loaded to prevent duplicates on next build
		logrus.WithFields(logrus.Fields{
			"patterns_added": patternsAdded,
			"total_patterns": len(maskingPatterns),
		}).Info("loaded sanitize patterns from delegate")
	} else {
		logrus.Warn("no valid sanitize patterns loaded from delegate")
	}

	return nil
}

// GetMaskingPatternsCount returns the number of active masking patterns
// Useful for testing and diagnostics
func GetMaskingPatternsCount() int {
	return len(maskingPatterns)
}
