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
)

// PullPolicy defines the container image pull policy.
type PullPolicy int

// PullPolicy enumeration.
const (
	PullDefault PullPolicy = iota
	PullAlways
	PullIfNotExists
	PullNever
)

// CloudDriver defines the cloud driver.
const (
	CloudDriver = "CLOUD_DRIVER"
)

// AWS Credential Broker env vars consumed by the credential-broker binary
// (upstream: credentialBroker/cmd/internal/config/config.go in
//  git0.harness.io/l7B_kbSEQD2wjrM7PShm5w/PROD/Harness_Commons/credentialBroker).
// Their presence on a step is the contract used to gate broker-specific staging logic
// (binary mount, AWS_CONFIG_FILE injection) across lite-engine's container/host wiring,
// matching the env-var names emitted by the Harness Manager (see AwsConstants in
// harness-core/954-connector-beans).
const (
	AwsBrokerEndpointEnv = "BROKER_ENDPOINT"
)

func (p PullPolicy) String() string {
	return pullPolicyID[p]
}

var pullPolicyID = map[PullPolicy]string{
	PullDefault:     "default",
	PullAlways:      "Always",
	PullIfNotExists: "IfNotPresent",
	PullNever:       "Never",
}

var pullPolicyName = map[string]PullPolicy{
	"":             PullDefault,
	"default":      PullDefault,
	"Always":       PullAlways,
	"IfNotPresent": PullIfNotExists,
	"Never":        PullNever,
}

// MarshalJSON marshals the string representation of the
// pull type to JSON.
func (p *PullPolicy) MarshalJSON() ([]byte, error) {
	buffer := bytes.NewBufferString(`"`)
	buffer.WriteString(pullPolicyID[*p])
	buffer.WriteString(`"`)
	return buffer.Bytes(), nil
}

// UnmarshalJSON unmarshals the json representation of the
// pull type from a string value.
func (p *PullPolicy) UnmarshalJSON(b []byte) error {
	// unmarshal as string
	var s string
	err := json.Unmarshal(b, &s)
	if err != nil {
		return err
	}
	// lookup value
	*p = pullPolicyName[s]
	return nil
}
