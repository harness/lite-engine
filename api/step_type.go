// Copyright 2022 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package api

import (
	"bytes"
	"encoding/json"
)

// StepType defines the step type.
type StepType int

// StepType enumeration.
const (
	Run StepType = iota
	RunTest
)

func (s StepType) String() string {
	return stepTypeID[s]
}

var stepTypeID = map[StepType]string{
	Run:     "Run",
	RunTest: "RunTest",
}

var stepTypeName = map[string]StepType{
	"":        Run,
	"Run":     Run,
	"RunTest": RunTest,
}

// MarshalJSON marshals the string representation of the
// step type to JSON.
func (s *StepType) MarshalJSON() ([]byte, error) {
	buffer := bytes.NewBufferString(`"`)
	buffer.WriteString(stepTypeID[*s])
	buffer.WriteString(`"`)
	return buffer.Bytes(), nil
}

// UnmarshalJSON unmarshals the json representation of the
// step type from a string value.
func (s *StepType) UnmarshalJSON(b []byte) error {
	// unmarshal as string
	var a string
	err := json.Unmarshal(b, &a)
	if err != nil {
		return err
	}
	// lookup value
	*s = stepTypeName[a]
	return nil
}
