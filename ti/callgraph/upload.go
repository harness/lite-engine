// Copyright 2022 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package callgraph

import (
	"context" // Added for logging deserialized callgraph
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
func Upload(ctx context.Context, stepID string, timeMs int64, log *logrus.Logger, start time.Time, cfg *tiCfg.Cfg, dir string, uniqueStepID string, hasFailed bool, tests []*types.TestCase) error {
	if cfg.GetIgnoreInstr() {
		log.Infoln("Skipping call graph collection since instrumentation was ignored")
		return nil
	}
	// Create step-specific data directory path
	stepDataDir := filepath.Join(cfg.GetDataDir(), instrumentation.GetUniqueHash(uniqueStepID, cfg))

	encCg, cgIsEmpty, err := encodeCg(fmt.Sprintf(dir, stepDataDir), log, tests, "1_1")
	if err != nil {
		return errors.Wrap(err, "failed to get avro encoded callgraph")
	}

	c := cfg.GetClient()

	if hasFailed {
		if cgErr := c.UploadCgFailedTest(ctx, stepID, cfg.GetSourceBranch(), cfg.GetTargetBranch(), timeMs, encCg); cgErr != nil {
			return cgErr
		}
	} else if !cgIsEmpty {
		if cgErr := c.UploadCg(ctx, stepID, cfg.GetSourceBranch(), cfg.GetTargetBranch(), timeMs, encCg); cgErr != nil {
			log.Warnln("Failed to upload callgraph with latest version, trying with older version", cgErr)
			// try with version ""
			encCg, cgIsEmpty, avroErr := encodeCg(fmt.Sprintf(dir, stepDataDir), log, tests, "")
			if avroErr != nil {
				return errors.Wrap(avroErr, "failed to get avro encoded callgraph")
			}
			if !cgIsEmpty {
				if cgErr := c.UploadCg(ctx, stepID, cfg.GetSourceBranch(), cfg.GetTargetBranch(), timeMs, encCg); cgErr != nil {
					return cgErr
				}
			}
		}
	}

	log.Infoln(fmt.Sprintf("Successfully uploaded callgraph in %s time", time.Since(start)))
	return nil
}

// encodeCg reads all files of specified format from datadir folder and returns byte array of avro encoded format
func encodeCg(dataDir string, log *logrus.Logger, tests []*types.TestCase, version string) (data []byte, isEmpty bool, err error) {
	var parser Parser
	var cgIsEmpty bool
	fs := filesystem.New()

	if dataDir == "" {
		return nil, cgIsEmpty, fmt.Errorf("dataDir not present in request")
	}
	cgFiles, visFiles, err := getCgFiles(dataDir, "json", "csv", log)
	if err != nil {
		return nil, cgIsEmpty, errors.Wrap(err, "failed to fetch files inside the directory")
	}
	parser = NewCallGraphParser(log, fs)
	cg, err := parser.Parse(cgFiles, visFiles)
	for i := range cg.Nodes {
		cg.Nodes[i].HasFailed = false // Initialize HasFailed for the current node
		for _, test := range tests {
			fqcn := fmt.Sprintf("%s.%s", cg.Nodes[i].Package, cg.Nodes[i].Class)
			if fqcn == test.ClassName && cg.Nodes[i].Method == test.Name {
				cg.Nodes[i].HasFailed = string(test.Result.Status) == string(types.StatusFailed)
			}
		}
	}
	if err != nil {
		return nil, cgIsEmpty, errors.Wrap(err, "failed to parse visgraph")
	}
	log.Infoln(fmt.Sprintf("Size of Test nodes: %d, Test relations: %d, Vis Relations %d", len(cg.Nodes), len(cg.TestRelations), len(cg.VisRelations)))

	if isCgEmpty(cg) {
		cgIsEmpty = true
	}
	cgMap := cg.ToStringMap()
	cgSer, err := avro.NewCgphSerialzer(cgSchemaType, version)
	if err != nil {
		return nil, cgIsEmpty, errors.Wrap(err, "failed to create serializer")
	}
	encCg, err := cgSer.Serialize(cgMap)
	if err != nil {
		return nil, cgIsEmpty, errors.Wrap(err, "failed to encode callgraph")
	}
	return encCg, cgIsEmpty, nil
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
