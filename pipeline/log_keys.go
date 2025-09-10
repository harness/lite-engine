package pipeline

import (
	"sync"
)

var (
	// openedLogKeys tracks which log keys have already been opened
	openedLogKeys      = make(map[string]bool)
	openedLogKeysMutex sync.RWMutex
)

// LogKeyExists checks if a log key has already been opened
func LogKeyExists(key string) bool {
	openedLogKeysMutex.RLock()
	defer openedLogKeysMutex.RUnlock()
	return openedLogKeys[key]
}

// AddLogKey marks a log key as opened
func AddLogKey(key string) {
	openedLogKeysMutex.Lock()
	defer openedLogKeysMutex.Unlock()
	openedLogKeys[key] = true
}

// RemoveLogKey removes a log key from tracking
func RemoveLogKey(key string) {
	openedLogKeysMutex.Lock()
	defer openedLogKeysMutex.Unlock()
	delete(openedLogKeys, key)
}
