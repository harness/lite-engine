package handler

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/harness/lite-engine/errors"
	"github.com/sirupsen/logrus"
)

// unix epoch time
var epoch = time.Unix(0, 0).Format(time.RFC1123)

// http headers to disable caching.
var noCacheHeaders = map[string]string{
	"Expires":         epoch,
	"Cache-Control":   "no-cache, private, max-age=0",
	"Pragma":          "no-cache",
	"X-Accel-Expires": "0",
}

func WriteError(w http.ResponseWriter, err error) {
	if _, ok := err.(*errors.BadRequestError); ok {
		WriteBadRequest(w, err)
		return
	}

	if _, ok := err.(*errors.NotFoundError); ok {
		WriteNotFound(w, err)
		return
	}

	WriteInternalError(w, err)
}

// writeBadRequest writes the json-encoded error message
// to the response with a 400 bad request status code.
func WriteBadRequest(w http.ResponseWriter, err error) {
	writeError(w, err, http.StatusBadRequest)
}

// writeNotFound writes the json-encoded error message to
// the response with a 404 not found status code.
func WriteNotFound(w http.ResponseWriter, err error) {
	writeError(w, err, http.StatusNotFound)
}

// writeInternalError writes the json-encoded error message
// to the response with a 500 internal server error.
func WriteInternalError(w http.ResponseWriter, err error) {
	writeError(w, err, http.StatusInternalServerError)
}

// writeJSON writes the json-encoded representation of v to
// the response body.
func WriteJSON(w http.ResponseWriter, v interface{}, status int) {
	for k, val := range noCacheHeaders {
		w.Header().Set(k, val)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		logrus.WithError(err).WithField("v", v).Errorln("failed to encode response")
	}
}

// writeError writes the json-encoded error message to the
// response.
func writeError(w http.ResponseWriter, err error, status int) {
	out := struct {
		Message string `json:"error_msg"`
	}{err.Error()}
	WriteJSON(w, &out, status)
}
