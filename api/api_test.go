// Copyright 2026 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package api

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestEgressPolicy_MarshalUnmarshal ensures the reshaped struct round-trips
// cleanly through JSON with the field names we ship on the wire.
func TestEgressPolicy_MarshalUnmarshal(t *testing.T) {
	in := EgressPolicy{Username: "acct-abc", Password: "tok-xyz"}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got := string(b)
	if !strings.Contains(got, `"username":"acct-abc"`) || !strings.Contains(got, `"password":"tok-xyz"`) {
		t.Fatalf("unexpected JSON: %s", got)
	}
	// Confirm the old field names are gone.
	if strings.Contains(got, "enabled") || strings.Contains(got, "allowed_ips") {
		t.Fatalf("legacy fields leaked: %s", got)
	}

	var out EgressPolicy
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out != in {
		t.Fatalf("roundtrip mismatch: %+v vs %+v", out, in)
	}
}

// TestStartStepRequest_EgressPolicyRoundtrip verifies that the newly-added
// EgressPolicy field on StartStepRequest survives JSON marshal/unmarshal
// under the "egress_policy" key.
func TestStartStepRequest_EgressPolicyRoundtrip(t *testing.T) {
	req := StartStepRequest{
		ID:   "step-1",
		Name: "run-build",
		EgressPolicy: &EgressPolicy{
			Username: "acct-abc",
			Password: "tok-xyz",
		},
	}
	b, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(b), `"egress_policy":{"username":"acct-abc","password":"tok-xyz"}`) {
		t.Fatalf("unexpected JSON: %s", string(b))
	}

	var out StartStepRequest
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.EgressPolicy == nil {
		t.Fatalf("egress_policy dropped on unmarshal")
	}
	if out.EgressPolicy.Username != "acct-abc" || out.EgressPolicy.Password != "tok-xyz" {
		t.Fatalf("egress_policy fields mismatched: %+v", out.EgressPolicy)
	}
}

// TestStartStepRequest_EgressPolicyOmitEmpty verifies that omitting the
// field produces a JSON without an "egress_policy" key at all — protects
// downstream consumers from receiving a null.
func TestStartStepRequest_EgressPolicyOmitEmpty(t *testing.T) {
	req := StartStepRequest{ID: "step-1", Name: "run-build"}
	b, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(b), "egress_policy") {
		t.Fatalf("expected egress_policy omitted, got: %s", string(b))
	}
}

// TestStepStatusConfig_TokenHash verifies the token_hash field shipped by the
// runner round-trips through JSON and stays absent when unset, so requests
// from older runners keep working unchanged.
func TestStepStatusConfig_TokenHash(t *testing.T) {
	in := StepStatusConfig{Token: "jwe", TokenHash: "hash-123", AccountID: "acct"}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(b), `"token_hash":"hash-123"`) {
		t.Fatalf("token_hash missing from JSON: %s", string(b))
	}
	var out StepStatusConfig
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out != in {
		t.Fatalf("roundtrip mismatch: %+v vs %+v", out, in)
	}

	legacy, err := json.Marshal(StepStatusConfig{Token: "jwe"})
	if err != nil {
		t.Fatalf("marshal legacy: %v", err)
	}
	if strings.Contains(string(legacy), "token_hash") {
		t.Fatalf("token_hash should be omitted when empty: %s", string(legacy))
	}
}
