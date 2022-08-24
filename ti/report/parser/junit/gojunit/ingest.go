// Copyright 2022 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

// Copyright Josh Komoroske. All rights reserved.
// Use of this source code is governed by the MIT license,
// a copy of which can be found in the LICENSE.txt file.

package gojunit

import (
	"strconv"
	"strings"
	"time"

	"github.com/harness/lite-engine/ti"
)

// findSuites performs a depth-first search through the XML document, and
// attempts to ingest any "testsuite" tags that are encountered.
func findSuites(nodes []xmlNode, suites chan Suite) {
	for _, node := range nodes {
		switch node.XMLName.Local {
		case "testsuite":
			suites <- ingestSuite(node)
		default:
			findSuites(node.Nodes, suites)
		}
	}
}

func ingestSuite(root xmlNode) Suite { //nolint:gocritic
	suite := Suite{
		Name:       root.Attr("name"),
		Package:    root.Attr("package"),
		Properties: root.Attrs,
	}

	for _, node := range root.Nodes {
		switch node.XMLName.Local {
		case "testsuite":
			testsuite := ingestSuite(node)
			suite.Suites = append(suite.Suites, testsuite)
		case "testcase":
			testcase := ingestTestcase(node)
			suite.Tests = append(suite.Tests, testcase)
		case "properties":
			props := ingestProperties(node)
			suite.Properties = props
		case "system-out":
			suite.SystemOut = string(node.Content)
		case "system-err":
			suite.SystemErr = string(node.Content)
		}
	}

	suite.Aggregate()
	return suite
}

func ingestProperties(root xmlNode) map[string]string { //nolint:gocritic
	props := make(map[string]string, len(root.Nodes))

	for _, node := range root.Nodes {
		if node.XMLName.Local == "property" {
			name := node.Attr("name")
			value := node.Attr("value")
			props[name] = value
		}
	}

	return props
}

func ingestTestcase(root xmlNode) Test { //nolint:gocritic
	test := Test{
		Name:       root.Attr("name"),
		Classname:  root.Attr("classname"),
		DurationMs: duration(root.Attr("time")).Milliseconds(),
		Result:     ti.Result{Status: ti.StatusPassed},
		Properties: root.Attrs,
	}

	for _, node := range root.Nodes {
		switch node.XMLName.Local {
		case "skipped":
			test.Result.Status = ti.StatusSkipped
			test.Result.Message = node.Attr("message")
			test.Result.Desc = string(node.Content)
		case "failure":
			test.Result.Status = ti.StatusFailed
			test.Result.Message = node.Attr("message")
			test.Result.Type = node.Attr("type")
			test.Result.Desc = string(node.Content)
		case "error":
			test.Result.Status = ti.StatusError
			test.Result.Message = node.Attr("message")
			test.Result.Type = node.Attr("type")
			test.Result.Desc = string(node.Content)
		case "system-out":
			test.SystemOut = string(node.Content)
		case "system-err":
			test.SystemErr = string(node.Content)
		}
	}

	return test
}

func duration(t string) time.Duration {
	// Remove commas for larger durations
	t = strings.ReplaceAll(t, ",", "")

	// Check if there was a valid decimal value
	if s, err := strconv.ParseFloat(t, 64); err == nil { //nolint:gomnd
		return time.Duration(s*1000000) * time.Microsecond //nolint:gomnd
	}

	// Check if there was a valid duration string
	if d, err := time.ParseDuration(t); err == nil {
		return d
	}

	return 0
}
