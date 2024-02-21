// Copyright 2024 Harness Inc. All rights reserved.
// Use of this source code is governed by the PolyForm Free Trial 1.0.0 license
// that can be found in the licenses directory at the root of this repository, also available at
// https://polyformproject.org/wp-content/uploads/2020/05/PolyForm-Free-Trial-1.0.0.txt.

package runtime

import (
	"fmt"
	"log"

	"github.com/harness/lite-engine/internal/filesystem"
	"github.com/harness/ti-client/types"
)

func populateItemInFilterFile(selectTestResp types.SelectTestsResp, filePath string, fs filesystem.FileSystem, isFilterFilePresent bool) error {

	if !isFilterFilePresent { // If filter file is not present then we will simply run all the test cases
		log.Println("We are here where no filter file is being created") //Revert
		return nil
	}

	log.Println("Attempting to create filter file")
	testResp := selectTestResp.Tests
	filterFile := fmt.Sprintf("%s/filter", filePath)
	f, err := fs.Create(filterFile)
	if err != nil {
		log.Println(fmt.Sprintf("could not create file %s", filterFile), err)
		return err
	}

	var data string
	for i := range testResp {
		test := testResp[i]
		if test.Pkg != "" && test.Class != "" && test.Method != "" {
			data += fmt.Sprintf("%s.%s,%s\n", test.Pkg, test.Class, test.Method)
		} else if test.Pkg != "" && test.Class != "" {
			data += fmt.Sprintf("%s.%s\n", test.Pkg, test.Class)
		} else if test.Pkg != "" {
			data += fmt.Sprintf("%s\n", test.Pkg)
		}
	}

	_, err = f.Write([]byte(data))
	if err != nil {
		log.Println(fmt.Sprintf("could not write %s to file %s", data, filterFile), err)
		return err
	}

	return nil
}
