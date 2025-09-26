// Copyright 2024 Harness Inc. All rights reserved.
// Use of this source code is governed by the PolyForm Free Trial 1.0.0 license
// that can be found in the licenses directory at the root of this repository, also available at
// https://polyformproject.org/wp-content/uploads/2020/05/PolyForm-Free-Trial-1.0.0.txt.

package testsfilteration

import (
	"fmt"
	"log"

	"github.com/harness/lite-engine/internal/filesystem"
	"github.com/harness/ti-client/types"
)

func PopulateItemInFilterFile(selectTestResp types.SelectTestsResp, filterFilePath string, fs filesystem.FileSystem, isFilterFilePresent bool) error {
	if !isFilterFilePresent { // If filter file is not present then we will simply run all the test cases
		log.Println("Filter File not present running all the tests")
		return nil
	}
	testResp := selectTestResp.Tests

	f, err := fs.Create(filterFilePath)
	if err != nil {
		log.Println(fmt.Sprintf("could not create file %s", filterFilePath), err)
		return err
	}
	defer f.Close()

	var data string
	for i := range testResp {
		test := testResp[i]
		if test.Pkg != "" && test.Class != "" && test.Method != "" {
			data += fmt.Sprintf("%s.%s,%s\n", test.Pkg, test.Class, test.Method)
		} else if test.Pkg != "" && test.Class != "" {
			data += fmt.Sprintf("%s.%s\n", test.Pkg, test.Class)
		} else if test.Pkg != "" {
			data += fmt.Sprintf("%s\n", test.Pkg)
		} else if test.Class != "" {
			data += fmt.Sprintf("%s\n", test.Class)
		}
	}

	_, err = f.WriteString(data)
	if err != nil {
		log.Println(fmt.Sprintf("could not write %s to file %s", data, filterFilePath), err)
		return err
	}

	return nil
}
