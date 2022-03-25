// Copyright 2022 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

// Copyright Josh Komoroske. All rights reserved.
// Use of this source code is governed by the MIT license,
// a copy of which can be found in the LICENSE.txt file.

package gojunit

import (
	"github.com/harness/lite-engine/ti"
)

// Totals contains aggregated results across a set of test runs. Is usually
// calculated as a sum of all given test runs, and overrides whatever was given
// at the suite level.
//
// The following relation should hold true.
//   Tests == (Passed + Skipped + Failed + Error)
type Totals struct {
	// Tests is the total number of tests run.
	Tests int `json:"tests" yaml:"tests"`

	// Passed is the total number of tests that passed successfully.
	Passed int `json:"passed" yaml:"passed"`

	// Skipped is the total number of tests that were skipped.
	Skipped int `json:"skipped" yaml:"skipped"`

	// Failed is the total number of tests that resulted in a failure.
	Failed int `json:"failed" yaml:"failed"`

	// Error is the total number of tests that resulted in an error.
	Error int `json:"error" yaml:"error"`

	// DurationMs is the total time taken to run all tests in milliseconds
	DurationMs int64 `json:"duration" yaml:"duration"`
}

// Suite represents a logical grouping (suite) of tests.
type Suite struct {
	// Name is a descriptor given to the suite.
	Name string `json:"name" yaml:"name"`

	// Package is an additional descriptor for the hierarchy of the suite.
	Package string `json:"package" yaml:"package"`

	// Properties is a mapping of key-value pairs that were available when the
	// tests were run.
	Properties map[string]string `json:"properties,omitempty" yaml:"properties,omitempty"`

	// Tests is an ordered collection of tests with associated results.
	Tests []Test `json:"tests,omitempty" yaml:"tests,omitempty"`

	// Suites is an ordered collection of suites with associated tests.
	Suites []Suite `json:"suites,omitempty" yaml:"suites,omitempty"`

	// SystemOut is textual test output for the suite. Usually output that is
	// written to stdout.
	SystemOut string `json:"stdout,omitempty" yaml:"stdout,omitempty"`

	// SystemErr is textual test error output for the suite. Usually output that is
	// written to stderr.
	SystemErr string `json:"stderr,omitempty" yaml:"stderr,omitempty"`

	// Totals is the aggregated results of all tests.
	Totals Totals `json:"totals" yaml:"totals"`
}

// Aggregate calculates result sums across all tests and nested suites.
func (s *Suite) Aggregate() {
	totals := Totals{Tests: len(s.Tests)}

	for _, test := range s.Tests { // nolint:gocritic
		totals.DurationMs += test.DurationMs
		switch test.Result.Status {
		case ti.StatusPassed:
			totals.Passed++
		case ti.StatusSkipped:
			totals.Skipped++
		case ti.StatusFailed:
			totals.Failed++
		case ti.StatusError:
			totals.Error++
		}
	}

	// just summing totals from nested suites
	for _, suite := range s.Suites { // nolint:gocritic
		suite.Aggregate()
		totals.Tests += suite.Totals.Tests
		totals.DurationMs += suite.Totals.DurationMs
		totals.Passed += suite.Totals.Passed
		totals.Skipped += suite.Totals.Skipped
		totals.Failed += suite.Totals.Failed
		totals.Error += suite.Totals.Error
	}

	s.Totals = totals
}

// Test represents the results of a single test run.
type Test struct {
	// Name is a descriptor given to the test.
	Name string `json:"name" yaml:"name"`

	// Classname is an additional descriptor for the hierarchy of the test.
	Classname string `json:"classname" yaml:"classname"`

	// DurationMs is the total time taken to run the tests in milliseconds.
	DurationMs int64 `json:"duration_ms" yaml:"duration"`

	// Result contains information related to the status of the test execution.
	Result ti.Result `json:"Result" yaml:"Result"`

	// Additional properties from XML node attributes.
	// Some tools use them to store additional information about test location.
	Properties map[string]string `json:"properties" yaml:"properties"`

	// SystemOut is textual output for the test case. Usually output that is
	// written to stdout.
	SystemOut string `json:"stdout,omitempty" yaml:"stdout,omitempty"`

	// SystemErr is textual error output for the test case. Usually output that is
	// written to stderr.
	SystemErr string `json:"stderr,omitempty" yaml:"stderr,omitempty"`
}
