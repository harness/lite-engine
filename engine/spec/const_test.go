// Copyright 2022 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

// Copyright 2019 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package spec

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestPullPolicy_Marshal(t *testing.T) {
	tests := []struct {
		policy PullPolicy
		data   string
	}{
		{
			policy: PullAlways,
			data:   `"Always"`,
		},
		{
			policy: PullDefault,
			data:   `"default"`,
		},
		{
			policy: PullIfNotExists,
			data:   `"IfNotPresent"`,
		},
		{
			policy: PullNever,
			data:   `"Never"`,
		},
	}
	for _, test := range tests {
		data, err := json.Marshal(&test.policy) //nolint:gosec // G601: Test code, aliasing is intentional
		if err != nil {
			t.Error(err)
			return
		}
		if bytes.Equal([]byte(test.data), data) == false {
			t.Errorf("Failed to marshal policy %s", test.policy)
		}
	}
}

func TestPullPolicy_Unmarshal(t *testing.T) {
	tests := []struct {
		policy PullPolicy
		data   string
	}{
		{
			policy: PullAlways,
			data:   `"Always"`,
		},
		{
			policy: PullDefault,
			data:   `"default"`,
		},
		{
			policy: PullIfNotExists,
			data:   `"IfNotPresent"`,
		},
		{
			policy: PullNever,
			data:   `"Never"`,
		},
		{
			// no policy should default to on-success
			policy: PullDefault,
			data:   `""`,
		},
	}
	for _, test := range tests {
		var policy PullPolicy
		err := json.Unmarshal([]byte(test.data), &policy)
		if err != nil {
			t.Error(err)
			return
		}
		if got, want := policy, test.policy; got != want {
			t.Errorf("Want policy %q, got %q", want, got)
		}
	}
}

func TestPullPolicy_UnmarshalTypeError(t *testing.T) {
	var policy PullPolicy
	err := json.Unmarshal([]byte("[]"), &policy)
	if _, ok := err.(*json.UnmarshalTypeError); !ok {
		t.Errorf("Expect unmarshal error return when JSON invalid")
	}
}

func TestPullPolicy_String(t *testing.T) {
	tests := []struct {
		policy PullPolicy
		value  string
	}{
		{
			policy: PullAlways,
			value:  "Always",
		},
		{
			policy: PullDefault,
			value:  "default",
		},
		{
			policy: PullIfNotExists,
			value:  "IfNotPresent",
		},
		{
			policy: PullNever,
			value:  "Never",
		},
	}
	for _, test := range tests {
		if got, want := test.policy.String(), test.value; got != want {
			t.Errorf("Want policy string %q, got %q", want, got)
		}
	}
}
