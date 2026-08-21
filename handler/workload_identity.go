// Copyright 2026 Harness Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package handler

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/harness/lite-engine/api"
	"github.com/harness/lite-engine/logger"
	pruntime "github.com/harness/lite-engine/pipeline/runtime"
)

// HandleMintWorkloadToken brokers an OIDC token mint for a step's declared workload identity. hcli
// inside the step POSTs its handle + identity name; lite-engine resolves the held workload token and
// mints against HarnessID. The workload token never leaves lite-engine - only the minted OIDC token is
// returned. The opaque per-step handle is the capability that authorizes the mint.
func HandleMintWorkloadToken() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		st := time.Now()

		var req api.MintWorkloadTokenRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			WriteBadRequest(w, err)
			return
		}

		resp := pruntime.MintWorkloadToken(r.Context(), req)
		status := http.StatusOK
		switch {
		case resp.Error == pruntime.ErrUnknownWorkloadIdentity:
			// Unknown handle/name is a not-found (client) condition, not an internal mint failure.
			status = http.StatusNotFound
		case resp.Error != "":
			status = http.StatusInternalServerError
		}
		WriteJSON(w, resp, status)

		logger.FromRequest(r).
			WithField("latency", time.Since(st)).
			WithField("time", time.Now().Format(time.RFC3339)).
			WithField("name", req.Name).
			Infoln("api: workload token mint handled")
	}
}
