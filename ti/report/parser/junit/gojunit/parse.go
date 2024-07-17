// Copyright 2022 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

// Copyright Josh Komoroske. All rights reserved.
// Use of this source code is governed by the MIT license,
// a copy of which can be found in the LICENSE.txt file.

package gojunit

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"html"
	"io"
	"time"
)

// reparentXML will wrap the given reader (which is assumed to be valid XML),
// in a fake root nodeAlias.
//
// This action is useful in the event that the original XML document does not
// have a single root nodeAlias, which is required by the XML specification.
// Additionally, Go's XML parser will silently drop all nodes after the first
// that is encountered, which can lead to data loss from a parser perspective.
// This function also enables the ingestion of blank XML files, which would
// normally cause a parsing error.
func reparentXML(reader io.Reader) io.Reader {
	return io.MultiReader(
		bytes.NewReader([]byte("<fake-root>")),
		reader,
		bytes.NewReader([]byte("</fake-root>")),
	)
}

// extractContent parses the raw contents from an XML node, and returns it in a
// more consumable form.
//
// This function deals with two distinct classes of node data; Encoded entities
// and CDATA tags. These Encoded entities are normal (html escaped) text that
// you typically find between tags like so:
// • "Hello, world!"  →  "Hello, world!"
// • "I &lt;/3 XML"   →  "I </3 XML"
// CDATA tags are a special way to embed data that would normally require
// escaping, without escaping it, like so:
// • "<![CDATA[Hello, world!]]>"  →  "Hello, world!"
// • "<![CDATA[I &lt;/3 XML]]>"   →  "I &lt;/3 XML"
// • "<![CDATA[I </3 XML]]>"      →  "I </3 XML"
//
// This function specifically allows multiple interleaved instances of either
// encoded entities or cdata, and will decode them into one piece of normalized text, like so:
//   - "I &lt;/3 XML <![CDATA[a lot]]>. You probably <![CDATA[</3 XML]]> too."  →  "I </3 XML a lot. You probably </3 XML too."
//     └─────┬─────┘         └─┬─┘   └──────┬──────┘         └──┬──┘   └─┬─┘
//     "I </3 XML "            │     ". You probably "          │      " too."
//     "a lot"                         "</3 XML"
//
// Errors are returned only when there are unmatched CDATA tags, although these
// should cause proper XML unmarshalling errors first, if encountered in an
// actual XML document.
func extractContent(data []byte) ([]byte, error) {
	var (
		cdataStart = []byte("<![CDATA[")
		cdataEnd   = []byte("]]>")
		mode       int
		output     []byte
	)

	for {
		if mode == 0 {
			offset := bytes.Index(data, cdataStart)
			if offset == -1 {
				// The string "<![CDATA[" does not appear in the data. Unescape all remaining data and finish
				if bytes.Contains(data, cdataEnd) {
					// The string "]]>" appears in the data. This is an error!
					return nil, errors.New("unmatched CDATA end tag")
				}

				output = append(output, html.UnescapeString(string(data))...)
				break
			}

			// The string "<![CDATA[" appears at some offset. Unescape up to that offset. Discard "<![CDATA[" prefix.
			output = append(output, html.UnescapeString(string(data[:offset]))...)
			data = data[offset:]
			data = data[9:]
			mode = 1
		} else if mode == 1 {
			offset := bytes.Index(data, cdataEnd)
			if offset == -1 {
				// The string "]]>" does not appear in the data. This is an error!
				return nil, errors.New("unmatched CDATA start tag")
			}

			// The string "]]>" appears at some offset. Read up to that offset. Discard "]]>" prefix.
			output = append(output, data[:offset]...)
			data = data[offset:]
			data = data[3:]
			mode = 0
		}
	}

	return output, nil
}

// parse unmarshalls the given XML data into a graph of nodes, and then returns
// a slice of all top-level nodes.
func parse(reader io.Reader) ([]xmlNode, error) {
	var (
		dec  = xml.NewDecoder(reparentXML(reader))
		root xmlNode
	)

	if err := dec.Decode(&root); err != nil {
		return nil, err
	}

	return root.Nodes, nil
}

type unitTestResult struct {
	Message    string `xml:"Output>ErrorInfo>Message"`
	StackTrace string `xml:"Output>ErrorInfo>StackTrace"`
	Outcome    string `xml:"outcome,attr"`
	TestID     string `xml:"testId,attr"`
	TestName   string `xml:"testName,attr"`
	EndTime    string `xml:"endTime,attr"`
	StartTime  string `xml:"startTime,attr"`
	Duration   string `xml:"duration,attr"`
}

type unitTest struct {
	ID     string     `xml:"id,attr"`
	Method testMethod `xml:"TestMethod"`
}

type testMethod struct {
	ClassName string `xml:"className,attr"`
}

// parse unmarshalls the given XML data into a graph of nodes, and then returns
// a slice of all top-level nodes.
func parseTrx(reader io.Reader) ([]xmlNode, error) {
	var (
		dec  = xml.NewDecoder(reader)
		root xmlNode
	)

	root = xmlNode{XMLName: xml.Name{Local: "fake-root"}}

	root.Nodes = append(root.Nodes, xmlNode{XMLName: xml.Name{Local: "testsuites"}})
	testSuites := &root.Nodes[0]
	testSuites.Nodes = append(testSuites.Nodes, xmlNode{XMLName: xml.Name{Local: "testsuite"}})
	testSuite := &testSuites.Nodes[0]

	testSuite.Attrs = make(map[string]string)
	testSuite.Attrs["name"] = "MSTestSuite"

	testCases := make(map[string]int) // test id -> index

	tests := 0
	skipped := 0
	failed := 0
	errors := 0

	for {
		t, err := dec.Token()

		if t == nil || err != nil { // when t is nil, we finished reading the file
			break
		}

		switch se := t.(type) {
		case xml.StartElement:
			switch se.Name.Local {
			case "Times":
				handleTimesNode(&se, testSuite)

			case "UnitTestResult":
				tests++
				err := handleUnitTestResultNode(dec, &se, testSuite, testCases, &skipped, &failed, &errors)
				if err != nil {
					return nil, err
				}

			case "UnitTest":
				err := handleUnitTest(dec, &se, testSuite, testCases)
				if err != nil {
					return nil, err
				}
			default:
			}
		default:
		}
	}

	testSuite.Attrs["tests"] = fmt.Sprintf("%d", tests)
	testSuite.Attrs["skipped"] = fmt.Sprintf("%d", skipped)
	testSuite.Attrs["failed"] = fmt.Sprintf("%d", failed)
	testSuite.Attrs["errors"] = fmt.Sprintf("%d", errors)

	return root.Nodes, nil
}

func handleUnitTest(decoder *xml.Decoder, startElement *xml.StartElement, testSuite *xmlNode, testCases map[string]int) error {
	var u unitTest
	err := decoder.DecodeElement(&u, startElement)
	if err != nil {
		return err
	}

	testCaseIndex := testCases[u.ID]
	testCase := &testSuite.Nodes[testCaseIndex]
	testCase.Attrs["classname"] = u.Method.ClassName

	return nil
}

func handleUnitTestResultNode(decoder *xml.Decoder, startElement *xml.StartElement, testSuite *xmlNode, testCases map[string]int,
	skipped *int, failed *int, errors *int) error {
	var u unitTestResult
	err := decoder.DecodeElement(&u, startElement)
	if err != nil {
		return err
	}

	testSuite.Nodes = append(testSuite.Nodes, xmlNode{XMLName: xml.Name{Local: "testcase"}})
	testCase := &testSuite.Nodes[len(testSuite.Nodes)-1]
	testCases[u.TestID] = len(testSuite.Nodes) - 1
	testCase.Attrs = make(map[string]string)
	testCase.Attrs["id"] = u.TestID

	var finish time.Time
	var start time.Time
	var testDuration time.Time

	if u.Outcome == "Failed" {
		(*failed)++
		failure := xmlNode{XMLName: xml.Name{Local: "failure"}}
		testCase.Nodes = append(testCase.Nodes, failure)
		failure.Content = []byte(fmt.Sprintf(`MESSAGE:
		%s
		+++++++++++++++++++
		STACK TRACE:
		%s"`, u.Message, u.StackTrace))
	} else if u.Outcome == "" || u.Outcome == "Error" {
		(*errors)++
		errorNode := xmlNode{XMLName: xml.Name{Local: "error"}}
		testCase.Nodes = append(testCase.Nodes, errorNode)
		errorNode.Content = []byte(fmt.Sprintf(`MESSAGE:
		%s
		+++++++++++++++++++
		STACK TRACE:
		%s"`, u.Message, u.StackTrace))
	} else if u.Outcome != "Failed" && u.Outcome != "Passed" {
		(*skipped)++
	}

	testCase.Attrs["name"] = u.TestName
	testDuration, _ = time.Parse("15:04:05.9999999", u.Duration)

	testCase.Attrs["time"] = fmt.Sprintf("%f", float64(testDuration.Nanosecond()/int(time.Microsecond))/float64(time.Millisecond))

	return nil
}

func handleTimesNode(se *xml.StartElement, testSuite *xmlNode) {
	var finish time.Time
	var start time.Time

	for _, attr := range se.Attr {
		switch attr.Name.Local {
		case "finish":
			finish, _ = time.Parse("2006-01-02T15:04:05.9999999Z07:00", attr.Value)
		case "start":
			start, _ = time.Parse("2006-01-02T15:04:05.9999999Z07:00", attr.Value)
		default:
		}
	}

	duration := finish.Sub(start)
	testSuite.Attrs["time"] = fmt.Sprintf("%f", duration.Seconds())
}
