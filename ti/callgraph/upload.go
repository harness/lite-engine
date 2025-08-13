// Copyright 2022 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package callgraph

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/harness/lite-engine/api"
	"github.com/harness/lite-engine/internal/filesystem"
	"github.com/harness/lite-engine/ti/avro"
	tiCfg "github.com/harness/lite-engine/ti/config"
	"github.com/harness/lite-engine/ti/instrumentation"
	"github.com/harness/ti-client/chrysalis/types"
	tiClientUtils "github.com/harness/ti-client/chrysalis/utils"
	tiClientTypes "github.com/harness/ti-client/types"
	"github.com/mattn/go-zglob"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

const (
	cgSchemaType = "callgraph"
	cgDir        = "%s/ti/callgraph/" // path where callgraph files will be generated
)

// Upload method uploads the callgraph.
//
//nolint:gocritic // paramTypeCombine: keeping separate string parameters for clarity
func Upload(ctx context.Context, stepID string, timeMs int64, log *logrus.Logger, start time.Time, cfg *tiCfg.Cfg, dir string, uniqueStepID string, hasFailed bool, r *api.StartStepRequest) error {
	if cfg.GetIgnoreInstr() {
		log.Infoln("Skipping call graph collection since instrumentation was ignored")
		return nil
	}
	// Create step-specific data directory path
	stepDataDir := filepath.Join(cfg.GetDataDir(), instrumentation.GetUniqueHash(uniqueStepID, cfg))

	cg, err := parseCallgraphFiles(fmt.Sprintf(dir, stepDataDir), log)
	if err != nil {
		return errors.Wrap(err, "failed to parse callgraph files")
	}

	fileHashPairs, err := getGitFileChecksums(ctx, r.WorkingDir, log)
	if err != nil {
		return errors.Wrap(err, "failed to get file hashes")
	}

	uploadPayload := CreateUploadPayload(cg, fileHashPairs, r.TIConfig.Repo, cfg.GetAccountID(), cfg.GetOrgID(), cfg.GetProjectID(), r.TIConfig.Sha, log)

	err = cfg.GetClient().UploadCgV2(ctx, *uploadPayload)
	if err != nil {
		return errors.Wrap(err, "failed to upload callgraph")
	}

	/*encCg, cgIsEmpty, err := encodeCg(fmt.Sprintf(dir, stepDataDir), log)
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
			return cgErr
		}
	}*/

	log.Infoln(fmt.Sprintf("Successfully uploaded callgraph in %s time", time.Since(start)))
	return nil
}

// parseCallgraphFiles parses callgraph files from the specified data directory
func parseCallgraphFiles(dataDir string, log *logrus.Logger) (*Callgraph, error) {
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
	return cg, nil
}

// encodeCg reads all files of specified format from datadir folder and returns byte array of avro encoded format
func encodeCg(dataDir string, log *logrus.Logger) (data []byte, isEmpty bool, err error) {
	var cgIsEmpty bool

	cg, err := parseCallgraphFiles(dataDir, log)
	if err != nil {
		return nil, cgIsEmpty, err
	}
	log.Infoln(fmt.Sprintf("Size of Test nodes: %d, Test relations: %d, Vis Relations %d", len(cg.Nodes), len(cg.TestRelations), len(cg.VisRelations)))

	if isCgEmpty(cg) {
		cgIsEmpty = true
	}

	cgMap := cg.ToStringMap()
	cgSer, err := avro.NewCgphSerialzer(cgSchemaType)
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

func getGitFileChecksums(ctx context.Context, repoDir string, log *logrus.Logger) ([]tiClientTypes.FilehashPair, error) {
	log.Infof("Getting git file checksums from directory: %s", repoDir)

	// Execute git ls-tree -r HEAD . command in the specified directory
	cmd := exec.CommandContext(ctx, "git", "ls-tree", "-r", "HEAD", ".")
	cmd.Dir = repoDir

	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to execute git ls-tree command: %w", err)
	}

	// Parse the output and create file:checksum map
	fileHashPairs := make([]tiClientTypes.FilehashPair, 0)
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")

	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}

		// Git ls-tree output format: "<mode> <type> <checksum>\t<filepath>"
		// Example: "100644 blob a1b2c3d4e5f6... path/to/file.txt"
		parts := strings.Fields(line)
		if len(parts) < 4 {
			log.Warnf("Skipping malformed git ls-tree line: %s", line)
			continue
		}

		// Extract checksum (3rd field) and filepath (4th field onwards, joined with spaces)
		fullChecksum := parts[2]
		filepath := strings.Join(parts[3:], " ")

		// Take first 16 characters of 160-bit checksum and convert to uint64
		if len(fullChecksum) < 16 {
			log.Warnf("Skipping file with short checksum: %s (checksum: %s)", filepath, fullChecksum)
			continue
		}

		checksum64, err := strconv.ParseUint(fullChecksum[:16], 16, 64)
		if err != nil {
			log.Warnf("Failed to parse checksum for file %s: %v", filepath, err)
			continue
		}

		fileHashPairs = append(fileHashPairs, tiClientTypes.FilehashPair{
			Path:     filepath,
			Checksum: checksum64,
		})
	}

	log.Infof("Successfully processed %d files from git repository", len(fileHashPairs))
	return fileHashPairs, nil
}

func CreateUploadPayload(cg *Callgraph, fileHashPairs []tiClientTypes.FilehashPair, repo, account, org, project, commitSha string, log *logrus.Logger) *types.UploadCgRequest {
	// Create repository information
	repoInfo := types.Identifier{
		AccountID: account,
		OrgID:     org,
		ProjectID: project,
		Repo:      repo,
	}

	// Extract tests from call graph
	var tests []types.Test
	var chains []types.Chain

	if cg != nil {
		// Create a map of node ID to node for quick lookup
		nodeMap := make(map[int]Node)
		for _, node := range cg.Nodes {
			nodeMap[node.ID] = node
		}

		// Process call graph nodes to extract test information
		for _, node := range cg.Nodes {
			if node.Type == "test" { // Assuming test nodes have a specific type
				// Find connected sources for this test
				var sourcePaths []string
				for _, relation := range cg.TestRelations {
					// Check if this test is in the relation's tests
					for _, testID := range relation.Tests {
						if testID == node.ID {
							// Found a source connected to this test
							if sourceNode, exists := nodeMap[relation.Source]; exists {
								// Use the source file path if available, otherwise package + class
								if sourceNode.File != "" {
									sourcePaths = append(sourcePaths, sourceNode.File)
								} else {
									// Fallback to package.class format - validate both parts are not empty
									if sourceNode.Package != "" && sourceNode.Class != "" {
										sourcePaths = append(sourcePaths, sourceNode.Package+"."+sourceNode.Class)
									} else if sourceNode.Package != "" {
										sourcePaths = append(sourcePaths, sourceNode.Package)
									} else if sourceNode.Class != "" {
										sourcePaths = append(sourcePaths, sourceNode.Class)
									}
									// If both are empty, skip adding this source path
								}
							}
							break
						}
					}
				}

				// De-duplicate source paths to prevent redundant data
				uniquePaths := make(map[string]struct{})
				dedupedSourcePaths := make([]string, 0)
				for _, path := range sourcePaths {
					if _, exists := uniquePaths[path]; !exists {
						uniquePaths[path] = struct{}{}
						dedupedSourcePaths = append(dedupedSourcePaths, path)
					}
				}

				// If no sources found, use empty slice
				if len(dedupedSourcePaths) == 0 {
					sourcePaths = []string{}
				} else {
					sourcePaths = dedupedSourcePaths
				}

				// Use test file path if available, otherwise method name - validate not empty
				testPath := ""
				if node.File != "" {
					testPath = node.File
				} else if node.Method != "" {
					testPath = node.Method
				} else {
					// Fallback: use package.class.method or just package.class
					if node.Package != "" && node.Class != "" {
						testPath = node.Package + "." + node.Class
					} else if node.Package != "" {
						testPath = node.Package
					} else if node.Class != "" {
						testPath = node.Class
					} else {
						// Last resort: use node ID as string
						testPath = fmt.Sprintf("test_node_%d", node.ID)
					}
				}

				// Skip if testPath is still empty (shouldn't happen with fallbacks above)
				if testPath == "" {
					log.Warnf("Skipping test node with empty path: node_id=%d, type=%s", node.ID, node.Type)
					continue
				}

				// Validate commitSha - provide fallback if empty
				validCommitSha := commitSha
				if validCommitSha == "" {
					validCommitSha = "unknown_commit"
				}

				// Create test entry
				test := types.Test{
					Path:      testPath,
					ExtraInfo: map[string]string{},
					IndicativeChains: []types.IndicativeChain{
						{
							SourcePaths: sourcePaths, // Use connected source paths
						},
					},
				}
				tests = append(tests, test)

				// Create corresponding chain entry
				chain := types.Chain{
					Path:      testPath,
					Checksum:  strconv.FormatUint(tiClientUtils.ChainChecksum(fileHashPairs), 10),
					State:     types.TestState("SUCCESS"), // Always set to success as requested
					ExtraInfo: map[string]string{},
				}
				chains = append(chains, chain)
			}
		}
	}

	return &types.UploadCgRequest{
		Identifier: repoInfo,
		Tests:      tests,
		Chains:     chains,
	}
}
