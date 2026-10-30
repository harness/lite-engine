// Copyright 2026 Harness Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package remote

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/harness/lite-engine/logstream"
	"github.com/stretchr/testify/require"
)

type scopedClientFactory struct {
	name string
	new  func(endpoint, orgID, projectID string) logstream.Client
}

var scopedClientFactories = []scopedClientFactory{
	{
		name: "standard",
		new: func(endpoint, orgID, projectID string) logstream.Client {
			return NewHTTPClientWithLogScope(endpoint, "account", orgID, projectID, "token", false, false, "", "")
		},
	},
	{
		name: "optimized",
		new: func(endpoint, orgID, projectID string) logstream.Client {
			return NewOptimizedHTTPClientWithLogScope(endpoint, "account", orgID, projectID, "token", false, false, "", "")
		},
	},
}

type observedRequest struct {
	method   string
	path     string
	rawQuery string
	query    url.Values
}

func TestWriteAddsOnlyNonBlankLogScope(t *testing.T) {
	tests := []struct {
		name      string
		orgID     string
		projectID string
	}{
		{name: "both", orgID: "org / one", projectID: "project / one"},
		{name: "org only", orgID: "org"},
		{name: "project only", projectID: "project"},
		{name: "neither"},
	}

	for _, factory := range scopedClientFactories {
		t.Run(factory.name, func(t *testing.T) {
			for _, test := range tests {
				t.Run(test.name, func(t *testing.T) {
					requests := make(chan observedRequest, 1)
					server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						requests <- observeRequest(r)
						w.WriteHeader(http.StatusNoContent)
					}))
					defer server.Close()

					client := factory.new(server.URL, test.orgID, test.projectID)
					key := "account/pipeline/id/1/-exec/stage/step"
					err := client.Write(context.Background(), key, []*logstream.Line{{Message: "hello"}})
					require.NoError(t, err)

					request := <-requests
					require.Equal(t, http.MethodPut, request.method)
					require.Equal(t, "/stream", request.path)
					require.Equal(t, "account", request.query.Get("accountID"))
					require.Equal(t, key, request.query.Get("key"))
					require.Equal(t, test.orgID, request.query.Get("orgId"))
					require.Equal(t, test.projectID, request.query.Get("projectId"))
					require.Equal(t, test.orgID != "", request.query.Has("orgId"))
					require.Equal(t, test.projectID != "", request.query.Has("projectId"))

					expectedQuery := "accountID=account&key=" + key
					if test.orgID != "" {
						expectedQuery += "&orgId=" + url.QueryEscape(test.orgID)
					}
					if test.projectID != "" {
						expectedQuery += "&projectId=" + url.QueryEscape(test.projectID)
					}
					require.Equal(t, expectedQuery, request.rawQuery)
				})
			}
		})
	}
}

func TestLogScopeIsNotAddedToOpenOrClose(t *testing.T) {
	for _, factory := range scopedClientFactories {
		t.Run(factory.name, func(t *testing.T) {
			requests := make(chan observedRequest, 2)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests <- observeRequest(r)
				w.WriteHeader(http.StatusNoContent)
			}))
			defer server.Close()

			client := factory.new(server.URL, "org", "project")
			require.NoError(t, client.Open(context.Background(), "log-key"))
			require.NoError(t, client.Close(context.Background(), "log-key", false))

			for _, method := range []string{http.MethodPost, http.MethodDelete} {
				request := <-requests
				require.Equal(t, method, request.method)
				require.Equal(t, "/stream", request.path)
				require.Equal(t, "log-key", request.query.Get("key"))
				require.False(t, request.query.Has("orgId"))
				require.False(t, request.query.Has("projectId"))
			}
		})
	}
}

func observeRequest(r *http.Request) observedRequest {
	return observedRequest{
		method:   r.Method,
		path:     r.URL.Path,
		rawQuery: r.URL.RawQuery,
		query:    r.URL.Query(),
	}
}
