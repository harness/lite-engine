// Copyright 2022 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package logger

import (
	"context"
	"net/http"
	"time"

	"github.com/sirupsen/logrus"
)

type loggerKey struct{}

// L is an alias for the the standard logger.
var L = logrus.NewEntry(logrus.StandardLogger())

// WithContext returns a new context with the provided logger. Use in
// combination with logger.WithField(s) for great effect.
func WithContext(ctx context.Context, logger *logrus.Entry) context.Context {
	return context.WithValue(ctx, loggerKey{}, logger)
}

// FromContext retrieves the current logger from the context. If no
// logger is available, the default logger is returned.
func FromContext(ctx context.Context) *logrus.Entry {
	logger := ctx.Value(loggerKey{})
	if logger == nil {
		return L
	}
	return logger.(*logrus.Entry)
}

// FromRequest retrieves the current logger from the request. If no
// logger is available, the default logger is returned.
func FromRequest(r *http.Request) *logrus.Entry {
	return FromContext(r.Context())
}

func LogAndSerialize(entry *logrus.Entry, level logrus.Level, msg string) (string, error) {
	e := entry.WithTime(time.Now())
	e.Message = msg
	e.Level = level

	// log it normally
	entry.Log(level, msg)

	// serialize using the same formatter
	formatted, err := e.Logger.Formatter.Format(e)
	if err != nil {
		return "", err
	}
	return string(formatted), nil
}
