// Copyright 2025 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package errors

import (
	"sync"
)

// RulesCache provides thread-safe in-memory caching for parsed error rules.
// Cache is per-stage (lite-engine process lifetime) - cleared when stage ends.
type RulesCache struct {
	mu    sync.RWMutex
	rules map[string]string // key: yamlPath, value: parsed rules JSON
}

// globalCache is the singleton cache instance
var globalCache = &RulesCache{
	rules: make(map[string]string),
}

// GetGlobalCache returns the global rules cache instance
func GetGlobalCache() *RulesCache {
	return globalCache
}

// Get retrieves cached rules JSON for a given YAML path
func (c *RulesCache) Get(yamlPath string) (string, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	rules, exists := c.rules[yamlPath]
	return rules, exists
}

// Set stores parsed rules JSON for a given YAML path
func (c *RulesCache) Set(yamlPath string, rulesJSON string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.rules[yamlPath] = rulesJSON
}

// Clear removes all cached rules (called when stage ends)
func (c *RulesCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.rules = make(map[string]string)
}

// Size returns the number of cached entries
func (c *RulesCache) Size() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.rules)
}
