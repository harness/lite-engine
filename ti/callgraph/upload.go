// Copyright 2022 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package callgraph

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/harness/lite-engine/internal/filesystem"
	"github.com/harness/lite-engine/pipeline"
	"github.com/harness/lite-engine/ti/avro"
	"github.com/harness/lite-engine/ti/client"
	"github.com/harness/lite-engine/ti/instrumentation"
	"github.com/mattn/go-zglob"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

const (
	cgSchemaType = "callgraph"
	cgDir        = "%s/ti/callgraph/" // path where callgraph files will be generated
)

// Upload method uploads the callgraph.
func Upload(ctx context.Context, stepID string, timeMs int64, log *logrus.Logger) error {
	if instrumentation.IsManualExecution() {
		log.Infoln("Skipping call graph collection since it is a manual run")
		return nil
	}

	cfg := pipeline.GetState().GetTIConfig()
	if cfg == nil || cfg.URL == "" {
		return fmt.Errorf("TI config is not provided in setup")
	}

	source := cfg.SourceBranch
	if source == "" {
		return fmt.Errorf("source branch is not set")
	}
	target := cfg.TargetBranch
	if target == "" {
		return fmt.Errorf("target branch is not set")
	}

	encCg, err := encodeCg(fmt.Sprintf(cgDir, pipeline.SharedVolPath), log)
	if err != nil {
		return errors.Wrap(err, "failed to get avro encoded callgraph")
	}

	c := client.NewHTTPClient(cfg.URL, cfg.Token, cfg.AccountID, cfg.OrgID, cfg.ProjectID,
		cfg.PipelineID, cfg.BuildID, cfg.StageID, cfg.Repo, cfg.Sha, false)
	return c.UploadCg(ctx, stepID, source, target, timeMs, encCg)
}

// encodeCg reads all files of specified format from datadir folder and returns byte array of avro encoded format
func encodeCg(dataDir string, log *logrus.Logger) (
	[]byte, error) {
	var parser Parser
	fs := filesystem.New()

	if dataDir == "" {
		return nil, fmt.Errorf("dataDir not present in request")
	}
	cgFiles, visFiles, err := getCgFiles(dataDir, "json", "csv", log)
	if err != nil {
		return nil, errors.Wrap(err, "failed to fetch files inside the directory")
	}
	parser = NewCallGraphParser(log, fs)
	cg, err := parser.Parse(cgFiles, visFiles)
	if err != nil {
		return nil, errors.Wrap(err, "failed to parse visgraph")
	}
	log.Infoln(fmt.Sprintf("size of nodes: %d, testReln: %d, visReln %d", len(cg.Nodes), len(cg.TestRelations), len(cg.VisRelations)))

	cgMap := cg.ToStringMap()
	cgSer, err := avro.NewCgphSerialzer(cgSchemaType)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create serializer")
	}
	encCg, err := cgSer.Serialize(cgMap)
	if err != nil {
		return nil, errors.Wrap(err, "failed to encode callgraph")
	}
	return encCg, nil
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
