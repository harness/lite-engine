// Copyright 2026 Harness Inc. All rights reserved.
// Use of this source code is governed by the PolyForm Free Trial 1.0.0 license
// that can be found in the licenses directory at the root of this repository, also available at
// https://polyformproject.org/wp-content/uploads/2020/05/PolyForm-Free-Trial-1.0.0.txt.

package errors

import (
	"sync"

	"github.com/sirupsen/logrus"
)

var (
	cacheInstance *errorRulesCache
	cacheOnce     sync.Once
)

// errorRulesCache is a thread-safe cache for storing parsed error rules and parse failures
type errorRulesCache struct {
	mu     sync.RWMutex
	rules  map[string]*ErrorRules // key: yamlPath, value: parsed rules (wrapper containing config)
	errors map[string]error       // key: yamlPath, value: parse error (cached to prevent re-parsing invalid YAML)
}

// getCacheInstance returns the singleton cache instance
func getCacheInstance() *errorRulesCache {
	cacheOnce.Do(func() {
		cacheInstance = &errorRulesCache{
			rules:  make(map[string]*ErrorRules),
			errors: make(map[string]error),
		}
	})
	return cacheInstance
}

// GetCachedRulesOrParse atomically checks cache or coordinates parsing
// This prevents concurrent parsing of the same YAML file by using double-checked locking
// Parse failures are also cached to prevent repeated parsing of invalid YAML files
// parseFn is a function that parses the YAML file and returns the parsed rules
func GetCachedRulesOrParse(yamlPath string, parseFn func() (*ErrorRules, error)) (*ErrorRules, error) {
	cache := getCacheInstance()

	// Fast path: check cache with read lock
	cache.mu.RLock()
	if rules, exists := cache.rules[yamlPath]; exists && rules != nil {
		cache.mu.RUnlock()
		logrus.WithField("path", yamlPath).Infoln("TEST_YAML_CACHE: Cache HIT - returning cached rules")
		return rules, nil
	}
	// Check for cached parse errors
	if cachedErr, exists := cache.errors[yamlPath]; exists {
		cache.mu.RUnlock()
		logrus.WithField("path", yamlPath).Infoln("TEST_YAML_CACHE: Cache HIT - returning cached error")
		return nil, cachedErr
	}
	cache.mu.RUnlock()

	// Slow path: acquire write lock and double-check
	cache.mu.Lock()
	defer cache.mu.Unlock()

	// Double-check after acquiring write lock (another goroutine might have cached it)
	if rules, exists := cache.rules[yamlPath]; exists && rules != nil {
		logrus.WithField("path", yamlPath).Infoln("TEST_YAML_CACHE: Cache HIT (after lock) - returning cached rules")
		return rules, nil
	}
	// Double-check for cached errors
	if cachedErr, exists := cache.errors[yamlPath]; exists {
		logrus.WithField("path", yamlPath).Infoln("TEST_YAML_CACHE: Cache HIT (after lock) - returning cached error")
		return nil, cachedErr
	}

	logrus.WithField("path", yamlPath).Infoln("TEST_YAML_CACHE: Cache MISS - parsing YAML now")

	// Parse (only one goroutine reaches here, others wait and will get cached result)
	rules, err := parseFn()
	if err != nil {
		// Cache the parse error to prevent re-parsing invalid YAML
		cache.errors[yamlPath] = err
		return nil, err
	}

	if rules != nil {
		cache.rules[yamlPath] = rules
	}

	return rules, nil
}
