// Copyright 2021 Harness Inc. All rights reserved.
// Use of this source code is governed by the PolyForm Free Trial 1.0.0 license
// that can be found in the licenses directory at the root of this repository, also available at
// https://polyformproject.org/wp-content/uploads/2020/05/PolyForm-Free-Trial-1.0.0.txt.

package python

import (
	"fmt"

	"github.com/harness/harness-core/commons/go/lib/utils"
	"github.com/harness/harness-core/product/ci/common/external"
	"github.com/harness/lite-engine/ti"
)

var (
	getFiles     = utils.GetFiles
	getWorkspace = external.GetWrkspcPath
)

func getPythonTestsFromPattern(tests []ti.RunnableTest, workspace, testpattern string) ([]ti.RunnableTest, error) {
	files, err := getFiles(fmt.Sprintf("%s/**/%s", workspace, testpattern))
	if err != nil {
		return nil, err
	}

	for _, path := range files {
		if path == "" {
			continue
		}
		test := ti.RunnableTest{
			Class: path,
		}
		tests = append(tests, test)
	}
	return tests, nil
}

// GetPythonTests returns list of RunnableTests in the workspace with python extension.
// In case of errors, return empty list
func GetPythonTests(workspace string, testGlobs []string) []ti.RunnableTest {
	tests := make([]ti.RunnableTest, 0)
	tests, err := getPythonTestsFromPattern(tests, workspace, "test_*.py")
	if err != nil {
		return tests
	}
	tests, err = getPythonTestsFromPattern(tests, workspace, "*_test.py")
	if err != nil {
		return tests
	}

	return tests
}
