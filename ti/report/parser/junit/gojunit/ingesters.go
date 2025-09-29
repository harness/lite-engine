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
	"strings"

	"github.com/harness/lite-engine/internal/safego"
)

// IngestFile will parse the given XML file and return a slice of all contained
// JUnit test suite definitions.
func IngestFile(filename, rootSuiteName string) ([]Suite, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	return IngestReader(file, rootSuiteName, strings.HasSuffix(filename, ".trx"))
}

// IngestReader will parse the given XML reader and return a slice of all
// contained JUnit test suite definitions.
func IngestReader(reader io.Reader, rootSuiteName string, trxFormat bool) ([]Suite, error) {
	var (
		suiteChan = make(chan Suite)
		suites    = make([]Suite, 0)
		nodes     []xmlNode
		err       error
	)

	if trxFormat {
		nodes, err = parseTrx(reader)
	} else {
		nodes, err = parse(reader)
	}

	if err != nil {
		return nil, err
	}

	safego.SafeGo("junit_parser", func() {
		findSuites(nodes, suiteChan, "", rootSuiteName)
		close(suiteChan)
	})

	for suite := range suiteChan {
		suites = append(suites, suite)
	}

	return suites, nil
}

// Ingest will parse the given XML data and return a slice of all contained
// JUnit test suite definitions.
func Ingest(data []byte, rootSuiteName string) ([]Suite, error) {
	return IngestReader(bytes.NewReader(data), rootSuiteName, false)
}
