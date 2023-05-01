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

// GetPythonTests returns list of RunnableTests in the workspace with python extension.
// In case of errors, return empty list
func GetPythonTests(testGlobs []string) ([]ti.RunnableTest, error) {
	tests := make([]ti.RunnableTest, 0)
	wp, err := getWorkspace()
	if err != nil {
		return tests, err
	}

	files, _ := getFiles(fmt.Sprintf("%s/**/*.py", wp))
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
