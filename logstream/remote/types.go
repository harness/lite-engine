package remote

import "time"

// Error represents a json-encoded API error.
type Error struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *Error) Error() string {
	return e.Message
}

// Link represents a signed link.
type Link struct {
	Value   string        `json:"link"`
	Expires time.Duration `json:"expires"`
}

// Line represents a line in the logs.
type Line struct {
	Level     string            `json:"level"`
	Number    int               `json:"pos"`
	Message   string            `json:"out"`
	Timestamp time.Time         `json:"time"`
	Args      map[string]string `json:"args"`
}
