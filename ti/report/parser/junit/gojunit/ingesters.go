// Copyright 2022 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

// Copyright Josh Komoroske. All rights reserved.
// Use of this source code is governed by the MIT license,
// a copy of which can be found in the LICENSE.txt file.

package gojunit

import (
	"bytes"
	"io"
	"os"
)

const defaultRootSuiteName = "Root Suite"

func getRootSuiteName(envs map[string]string) string {
	if val, ok := envs["HARNESS_JUNIT_ROOT_SUITE_NAME"]; ok {
		return val
	}
	return defaultRootSuiteName
}

// IngestFile will parse the given XML file and return a slice of all contained
// JUnit test suite definitions.
func IngestFile(filename string, envs map[string]string) ([]Suite, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	return IngestReader(file, envs)
}

// IngestReader will parse the given XML reader and return a slice of all
// contained JUnit test suite definitions.
func IngestReader(reader io.Reader, envs map[string]string) ([]Suite, error) {
	var (
		suiteChan = make(chan Suite)
		suites    = make([]Suite, 0)
	)

	nodes, err := parse(reader)
	if err != nil {
		return nil, err
	}

	go func() {
		findSuites(nodes, suiteChan, "", getRootSuiteName(envs))
		close(suiteChan)
	}()

	for suite := range suiteChan {
		suites = append(suites, suite)
	}

	return suites, nil
}

// Ingest will parse the given XML data and return a slice of all contained
// JUnit test suite definitions.
func Ingest(data []byte, envs map[string]string) ([]Suite, error) {
	return IngestReader(bytes.NewReader(data), envs)
}
