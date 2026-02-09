// Copyright 2022 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package logger

import (
	"net/http"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
)

// Middleware provides logging middleware.
func Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-ID")
		if id == "" {
			newUUID, _ := uuid.NewRandom()
			id = newUUID.String()
		}
		ctx := r.Context()
		log := FromContext(ctx).WithField("request-id", id)
		log = log.WithFields(logrus.Fields{
			"method":  r.Method,
			"request": r.RequestURI,
			"remote":  r.RemoteAddr,
		})
		ctx = WithContext(ctx, log)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
