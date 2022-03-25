// Copyright 2022 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package logstream

import (
	"testing"
)

func TestReplace(t *testing.T) {
	secrets := []string{"correct-horse-batter-staple", ""}

	sw := &nopWriter{}
	w := NewReplacer(&nopCloser{sw}, secrets)
	w.Write([]byte("username octocat password correct-horse-batter-staple")) // nolint:errcheck
	w.Close()

	if got, want := sw.data[0], "username octocat password **************"; got != want {
		t.Errorf("Want masked string %s, got %s", want, got)
	}
}

func TestReplaceMultiline(t *testing.T) {
	key := `
-----BEGIN PRIVATE KEY-----
MIIBVQIBADANBgkqhkiG9w0BAQEFAASCAT8wggE7AgEAAkEA0SC5BIYpanOv6wSm
dHVVMRa+6iw/0aJpT9/LKcZ0XYQ43P9Vwn8c46MDvFJ+Uy41FwbxT+QpXBoLlp8D
sJY/dQIDAQABAkAesoL2GwtxSNIF2YTli2OZ9RDJJv2nNAPpaZxU4YCrST1AXGPB
tFm0LjYDDlGJ448syKRpdypAyCR2LidwrVRxAiEA+YU5Zv7bOwODCsmtQtIfBfhu
6SMBGMDijK7OYfTtjQsCIQDWjvly6b6doVMdNjqqTsnA8J1ShjSb8bFXkMels941
fwIhAL4Rr7I3PMRtXmrfSa325U7k+Yd59KHofCpyFiAkNLgVAiB8JdR+wnOSQAOY
loVRgC9LXa6aTp9oUGxeD58F6VK9PwIhAIDhSxkrIatXw+dxelt8DY0bEdDbYzky
r9nicR5wDy2W
-----END PRIVATE KEY-----`

	line := `> MIIBVQIBADANBgkqhkiG9w0BAQEFAASCAT8wggE7AgEAAkEA0SC5BIYpanOv6wSm`

	secrets := []string{key}

	sw := &nopWriter{}
	w := NewReplacer(&nopCloser{sw}, secrets)
	w.Write([]byte(line)) // nolint:errcheck
	w.Close()

	if got, want := sw.data[0], "> **************"; got != want {
		t.Errorf("Want masked string %s, got %s", want, got)
	}
}

func TestReplaceMultilineJson(t *testing.T) {
	key := `{
  "token":"MIIBVQIBADANBgkqhkiG9w0BAQEFAASCAT8wggE7AgEAAkEA0SC5BIYpanOv6wSm"
}`

	line := `{
  "token":"MIIBVQIBADANBgkqhkiG9w0BAQEFAASCAT8wggE7AgEAAkEA0SC5BIYpanOv6wSm"
}`

	secrets := []string{key}

	sw := &nopWriter{}
	w := NewReplacer(&nopCloser{sw}, secrets)
	w.Write([]byte(line)) // nolint:errcheck
	w.Close()

	if got, want := sw.data[0], "{\n  **************\n}"; got != want {
		t.Errorf("Want masked string %s, got %s", want, got)
	}
}

type nopCloser struct {
	Writer
}
