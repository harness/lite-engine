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
	Message string `xml:"Output>ErrorInfo>Message"`
	StackTrace string `xml:"Output>ErrorInfo>StackTrace"`
	Outcome string `xml:"outcome,attr"`
	TestId string `xml:"testId,attr"`
	TestName string `xml:"testName,attr"`
	EndTime string `xml:"endTime,attr"`
	StartTime string `xml:"startTime,attr"`
	Duration string `xml:"duration,attr"`
}

type unitTest struct {
	Id string `xml:"id,attr"`
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

	root = xmlNode{ XMLName: xml.Name{ Local: "fake-root" } }

	root.Nodes = append(root.Nodes, xmlNode{ XMLName: xml.Name{ Local: "testsuites" } })
	testSuites := &root.Nodes[0]
	testSuites.Nodes = append(testSuites.Nodes, xmlNode{ XMLName: xml.Name{ Local: "testsuite" } })
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
			case "UnitTestResult":
				tests += 1

				var u unitTestResult
				err := dec.DecodeElement(&u, &se)
				fmt.Println(err)
				fmt.Println(u)
				fmt.Println("Adding test case")

				testSuite.Nodes = append(testSuite.Nodes, xmlNode{ XMLName: xml.Name{ Local: "testcase" } })
				testCase := &testSuite.Nodes[len(testSuite.Nodes) - 1]
				testCases[u.TestId] = len(testSuite.Nodes) - 1
				testCase.Attrs = make(map[string]string)
				testCase.Attrs["id"] = u.TestId
				
				
				var finish time.Time
				var start time.Time
				var testDuration time.Time

				if u.Outcome == "Failed" {
					failed += 1
					failure := xmlNode{ XMLName: xml.Name{ Local: "failure" } }
					testCase.Nodes = append(testCase.Nodes, failure)
					failure.Content = []byte(fmt.Sprintf(`MESSAGE:
					%s
					+++++++++++++++++++
                    STACK TRACE:
					%s"`, u.Message, u.StackTrace))
				} else if u.Outcome == "" || u.Outcome == "Error" {
					errors += 1
					errorNode := xmlNode{ XMLName: xml.Name{ Local: "error" } }
					testCase.Nodes = append(testCase.Nodes, errorNode)
					errorNode.Content = []byte(fmt.Sprintf(`MESSAGE:
					%s
					+++++++++++++++++++
                    STACK TRACE:
					%s"`, u.Message, u.StackTrace))
				} else if u.Outcome != "Failed" && u.Outcome != "Passed" {
					skipped += 1
				}
				
				testCase.Attrs["name"] = u.TestName
				start, _ = time.Parse("2006-01-02T15:04:05.9999999Z07:00", u.StartTime)
				finish, _ = time.Parse("2006-01-02T15:04:05.9999999Z07:00", u.EndTime)
				testDuration, _ = time.Parse("15:04:05.9999999", u.Duration)

				duration := finish.Sub(start)
				testCase.Attrs["time"] = fmt.Sprintf("%f", float64(int(duration.Seconds())) + float64(testDuration.Nanosecond() / 1000) / 1000000)

			case "UnitTest":
				var u unitTest
				err := dec.DecodeElement(&u, &se)
				fmt.Println(err)

				fmt.Println(u.Id)
				fmt.Println(u.Method.ClassName)
				testCaseIndex := testCases[u.Id]
				testCase := &testSuite.Nodes[testCaseIndex]
				testCase.Attrs["classname"] = u.Method.ClassName
			default:
			}
		default:
		}
	}

	testSuite.Attrs["tests"] = fmt.Sprintf("%d", tests)
	testSuite.Attrs["skipped"] = fmt.Sprintf("%d", skipped)
	testSuite.Attrs["failed"] = fmt.Sprintf("%d", failed)
	testSuite.Attrs["errors"] = fmt.Sprintf("%d", errors)
	fmt.Println(testSuite.Attrs)

	return root.Nodes, nil
}

