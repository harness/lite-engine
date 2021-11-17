package api

import (
	"bytes"
	"encoding/json"
)

// ReportType defines the step type.
type ReportType int

// ReportType enumeration.
const (
	Junit ReportType = iota
)

func (s ReportType) String() string {
	return reportTypeID[s]
}

var reportTypeID = map[ReportType]string{
	Junit: "Junit",
}

var reportTypeName = map[string]ReportType{
	"":      Junit,
	"Junit": Junit,
}

// MarshalJSON marshals the string representation of the
// report type to JSON.
func (s *ReportType) MarshalJSON() ([]byte, error) {
	buffer := bytes.NewBufferString(`"`)
	buffer.WriteString(reportTypeID[*s])
	buffer.WriteString(`"`)
	return buffer.Bytes(), nil
}

// UnmarshalJSON unmarshals the json representation of the
// report type from a string value.
func (s *ReportType) UnmarshalJSON(b []byte) error {
	// unmarshal as string
	var a string
	err := json.Unmarshal(b, &a)
	if err != nil {
		return err
	}
	// lookup value
	*s = reportTypeName[a]
	return nil
}
