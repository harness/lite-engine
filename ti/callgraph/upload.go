// Copyright 2022 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package callgraph

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"github.com/harness/lite-engine/internal/filesystem"
	"github.com/harness/lite-engine/ti/avro"
	tiCfg "github.com/harness/lite-engine/ti/config"
	"github.com/harness/lite-engine/ti/instrumentation"
	"github.com/harness/ti-client/types"
	"github.com/mattn/go-zglob"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

const (
	cgSchemaType = "callgraph"
	cgDir        = "%s/ti/callgraph/" // path where callgraph files will be generated
)

// Upload method uploads the callgraph.
func Upload(
	ctx context.Context,
	stepID string,
	timeMs int64,
	log *logrus.Logger,
	start time.Time,
	cfg *tiCfg.Cfg,
	dir, uniqueStepID string,
	tests []*types.TestCase,
	rerunFailedTests bool,
) error {
	if cfg.GetIgnoreInstr() {
		log.Infoln("Skipping call graph collection since instrumentation was ignored")
		return nil
	}
	// Create step-specific data directory path
	stepDataDir := filepath.Join(cfg.GetDataDir(), instrumentation.GetUniqueHash(uniqueStepID, cfg))

	encCg, cgIsEmpty, matched, err := encodeCg(fmt.Sprintf(dir, stepDataDir), log, tests, "1_1", rerunFailedTests)
	if err != nil {
		return errors.Wrap(err, "failed to get avro encoded callgraph")
	}

	c := cfg.GetClient()

	if !cgIsEmpty {
		if cgErr := c.UploadCg(ctx, stepID, cfg.GetSourceBranch(), cfg.GetTargetBranch(), timeMs, encCg, rerunFailedTests && matched); cgErr != nil {
			log.Warnln("Failed to upload callgraph with latest version, trying with older version", cgErr)
			// try with version ""
			encCg, cgIsEmpty, matched, avroErr := encodeCg(fmt.Sprintf(dir, stepDataDir), log, tests, "", rerunFailedTests)
			if avroErr != nil {
				return errors.Wrap(avroErr, "failed to get avro encoded callgraph")
			}
			if !cgIsEmpty {
				if cgErr := c.UploadCg(ctx, stepID, cfg.GetSourceBranch(), cfg.GetTargetBranch(), timeMs, encCg, rerunFailedTests && matched); cgErr != nil {
					return cgErr
				}
			}
		}
	}

	log.Infoln(fmt.Sprintf("Successfully uploaded callgraph in %s time", time.Since(start)))
	return nil
}

// encodeCg reads all files of specified format from datadir folder and returns byte array of avro encoded format
func encodeCg(dataDir string, log *logrus.Logger, tests []*types.TestCase, version string, rerunFailedTests bool) (data []byte, isEmpty, allMatched bool, err error) {
	var parser Parser
	var cgIsEmpty bool
	fs := filesystem.New()

	if dataDir == "" {
		return nil, cgIsEmpty, false, fmt.Errorf("dataDir not present in request")
	}
	cgFiles, visFiles, err := getCgFiles(dataDir, "json", "csv", log)
	if err != nil {
		return nil, cgIsEmpty, false, errors.Wrap(err, "failed to fetch files inside the directory")
	}
	parser = NewCallGraphParser(log, fs)
	cg, err := parser.Parse(cgFiles, visFiles)
	totalMatched := 0
	totalTests := 0
	if rerunFailedTests {
		for i := range cg.Nodes {
			cg.Nodes[i].HasFailed = false // Initialize HasFailed for the current node
			if cg.Nodes[i].Type != "test" {
				continue
			}
			totalTests++
			for _, test := range tests {
				fqcn := fmt.Sprintf("%s.%s", cg.Nodes[i].Package, cg.Nodes[i].Class)
				if fqcn == test.ClassName && cg.Nodes[i].Method == test.Name {
					cg.Nodes[i].HasFailed = string(test.Result.Status) == string(types.StatusFailed)
					// If a node has been run, the status should be either failed or passed, else the report does not match
					if test.Result.Status == types.StatusFailed || test.Result.Status == types.StatusPassed {
						totalMatched++
					}
					break
				}
			}
		}
	}
	allMatched = totalMatched == totalTests // To consider a report valid, all test nodes should be matched with valid reports
	if err != nil {
		return nil, cgIsEmpty, allMatched, errors.Wrap(err, "failed to parse visgraph")
	}
	log.Infoln(fmt.Sprintf("Size of Test nodes: %d, Test relations: %d, Vis Relations %d", len(cg.Nodes), len(cg.TestRelations), len(cg.VisRelations)))

	if isCgEmpty(cg) {
		cgIsEmpty = true
	}
	cgMap := cg.ToStringMap()
	cgSer, err := avro.NewCgphSerialzer(cgSchemaType, version)
	if err != nil {
		return nil, cgIsEmpty, false, errors.Wrap(err, "failed to create serializer")
	}
	encCg, err := cgSer.Serialize(cgMap)
	if err != nil {
		return nil, cgIsEmpty, false, errors.Wrap(err, "failed to encode callgraph")
	}
	return encCg, cgIsEmpty, allMatched, nil
}

func isCgEmpty(cg *Callgraph) bool {
	if len(cg.Nodes) == 0 && len(cg.TestRelations) == 0 && len(cg.VisRelations) == 0 {
		return true
	}
	return false
}

// get list of all file paths matching a provided regex
func getFiles(path string) ([]string, error) {
	matches, err := zglob.Glob(path)
	if err != nil {
		return []string{}, err
	}
	return matches, err
}

// getCgFiles return list of cg files in given directory
func getCgFiles(dir, ext1, ext2 string, log *logrus.Logger) ([]string, []string, error) { //nolint:gocritic,unparam
	cgFiles, err1 := getFiles(filepath.Join(dir, "**/*."+ext1))
	visFiles, err2 := getFiles(filepath.Join(dir, "**/*."+ext2))

	if err1 != nil || err2 != nil {
		log.Errorln(fmt.Sprintf("error in getting files list in dir %s", dir), err1, err2)
	}
	return cgFiles, visFiles, nil
}
