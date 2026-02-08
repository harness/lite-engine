// Copyright 2022 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

//nolint:gocyclo,gocritic
package callgraph

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
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
	tiClient "github.com/harness/ti-client/client"
	tiClientTypes "github.com/harness/ti-client/types"
	"github.com/mattn/go-zglob"
	"github.com/sirupsen/logrus"
)

const (
	cgSchemaType = "callgraph"
	cgDir        = "%s/ti/callgraph/"
	nodeTypeTest = "test"
)

var (
	// DetectedLanguages stores languages detected from callgraph for runtestV2
	DetectedLanguages []string
)

//nolint:gocyclo
func Upload(
	ctx context.Context,
	stepID string,
	timeMs int64,
	log *logrus.Logger,
	start time.Time,
	cfg *tiCfg.Cfg,
	dir, uniqueStepID string,
	tests []*tiClientTypes.TestCase,
	rerunFailedTests bool,
	r *api.StartStepRequest,
) (*Callgraph, error) {
	DetectedLanguages = []string{}
	if cfg.GetIgnoreInstr() {
		log.Infoln("Skipping call graph collection since instrumentation was ignored")
		return nil, nil
	}
	stepDataDir := filepath.Join(cfg.GetDataDir(), instrumentation.GetUniqueHash(uniqueStepID, cfg))

	if enhancedFFVal, ok := r.Envs["CI_TI_V2_ENHANCED_FF"]; ok && enhancedFFVal == "true" {
		var repo, sha string
		if httpClient, ok := cfg.GetClient().(*tiClient.HTTPClient); ok {
			repo = httpClient.Repo
			sha = httpClient.Sha
		} else {
			repo = ""
			sha = ""
		}
		cg, err := parseCallgraphFiles(fmt.Sprintf(dir, stepDataDir), log)
		if err != nil {
			return nil, fmt.Errorf("failed to parse callgraph files: %w", err)
		}
		fileChecksums, err := instrumentation.GetGitFileChecksums(ctx, r.WorkingDir, log)
		if err != nil {
			return nil, fmt.Errorf("failed to get file hashes: %w", err)
		}
		uploadPayload, err := CreateUploadPayload(cg, fileChecksums, repo, cfg, sha, tests, log, r.Envs)
		if err != nil {
			return nil, fmt.Errorf("failed to create upload payload: %w", err)
		}
		err = cfg.GetClient().UploadCgV2(ctx, *uploadPayload, stepID, timeMs, cfg.GetSourceBranch(), cfg.GetTargetBranch())
		if err != nil {
			return nil, fmt.Errorf("failed to upload callgraph: %w", err)
		}
	} else {
		encCg, cgIsEmpty, matched, err := encodeCg(fmt.Sprintf(dir, stepDataDir), log, tests, "1_1", rerunFailedTests)
		if err != nil {
			return nil, fmt.Errorf("failed to get avro encoded callgraph: %w", err)
		}

		c := cfg.GetClient()

		if !cgIsEmpty {
			if cgErr := c.UploadCg(ctx, stepID, cfg.GetSourceBranch(), cfg.GetTargetBranch(), timeMs, encCg, rerunFailedTests && matched); cgErr != nil {
				log.Warnln("Failed to upload callgraph with latest version, trying with older version", cgErr)
				// try with version ""
				encCg, cgIsEmpty, matched, avroErr := encodeCg(fmt.Sprintf(dir, stepDataDir), log, tests, "", rerunFailedTests)
				if avroErr != nil {
					return nil, fmt.Errorf("failed to get avro encoded callgraph: %w", avroErr)
				}
				if !cgIsEmpty {
					if cgErr := c.UploadCg(ctx, stepID, cfg.GetSourceBranch(), cfg.GetTargetBranch(), timeMs, encCg, rerunFailedTests && matched); cgErr != nil {
						return nil, cgErr
					}
				}
			}
		}
	}

	log.Infoln(fmt.Sprintf("Successfully uploaded callgraph in %s time", time.Since(start)))
	return nil, nil
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
		return nil, fmt.Errorf("failed to fetch files inside the directory: %w", err)
	}
	parser = NewCallGraphParser(log, fs)

	cg, err := parser.Parse(cgFiles, visFiles)
	if err != nil {
		return nil, fmt.Errorf("failed to parse visgraph: %w", err)
	}
	return cg, nil
}

//nolint:funlen
func encodeCg(dataDir string, log *logrus.Logger, tests []*tiClientTypes.TestCase, version string, rerunFailedTests bool) (data []byte, isEmpty, allMatched bool, err error) {
	var parser Parser
	var cgIsEmpty bool
	fs := filesystem.New()

	if dataDir == "" {
		return nil, cgIsEmpty, false, fmt.Errorf("dataDir not present in request")
	}
	cgFiles, visFiles, err := getCgFiles(dataDir, "json", "csv", log)
	if err != nil {
		return nil, cgIsEmpty, false, fmt.Errorf("failed to fetch files inside the directory: %w", err)
	}
	parser = NewCallGraphParser(log, fs)
	log.Infoln(fmt.Sprintf("Found callgraph files: %v", cgFiles))

	cg, err := parser.Parse(cgFiles, visFiles)
	if err != nil {
		return nil, cgIsEmpty, false, fmt.Errorf("failed to parse callgraph: %w", err)
	}

	log.Infof("Callgraph parsing completed. Total nodes: %d", len(cg.Nodes))
	languageSet := make(map[string]bool)

	// Handle failed test matching and language detection in a single loop
	totalMatched := 0
	totalTests := 0
	if rerunFailedTests {
		for i := range cg.Nodes {
			cg.Nodes[i].HasFailed = false // Initialize HasFailed for the current node
			if cg.Nodes[i].Type != nodeTypeTest {
				continue
			}
			if cg.Nodes[i].File != "" {
				ext := filepath.Ext(cg.Nodes[i].File)
				if ext != "" {
					languageSet[ext] = true
				}
			}
			totalTests++
			for _, test := range tests {
				fqcn := fmt.Sprintf("%s.%s", cg.Nodes[i].Package, cg.Nodes[i].Class)
				if fqcn == test.ClassName && cg.Nodes[i].Method == test.Name {
					cg.Nodes[i].HasFailed = string(test.Result.Status) == string(tiClientTypes.StatusFailed)
					// If a node has been run, the status should be either failed or passed, else the report does not match
					if test.Result.Status == tiClientTypes.StatusFailed || test.Result.Status == tiClientTypes.StatusPassed {
						totalMatched++
					}
					break
				}
			}
		}
	} else {
		for i := range cg.Nodes {
			if cg.Nodes[i].Type == nodeTypeTest && cg.Nodes[i].File != "" {
				ext := filepath.Ext(cg.Nodes[i].File)
				if ext != "" {
					languageSet[ext] = true
				}
			}
		}
	}
	if len(languageSet) > 0 {
		languages := make([]string, 0, len(languageSet))
		for lang := range languageSet {
			languages = append(languages, lang)
		}
		DetectedLanguages = languages
	}
	allMatched = totalMatched == totalTests // To consider a report valid, all test nodes should be matched with valid reports
	log.Infoln(fmt.Sprintf("Size of Test nodes: %d, Test relations: %d, Vis Relations %d", len(cg.Nodes), len(cg.TestRelations), len(cg.VisRelations)))

	if isCgEmpty(cg) {
		cgIsEmpty = true
	}
	cgMap := cg.ToStringMap()
	cgSer, err := avro.NewCgphSerialzer(cgSchemaType, version)
	if err != nil {
		return nil, cgIsEmpty, false, fmt.Errorf("failed to create serializer: %w", err)
	}
	encCg, err := cgSer.Serialize(cgMap)
	if err != nil {
		return nil, cgIsEmpty, false, fmt.Errorf("failed to encode callgraph: %w", err)
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

func findTestsForNode(tests []*tiClientTypes.TestCase, node *Node) []*tiClientTypes.TestCase {
	filePath := node.File
	fcqn := fmt.Sprintf("%s.%s", node.Package, node.Class)
	if strings.HasSuffix(node.Class, ".py") { // for python
		fcqn = strings.Replace(filePath, "/", ".", -1)
		fcqn = strings.TrimSuffix(fcqn, ".py")
	}

	filteredTests := make([]*tiClientTypes.TestCase, 0)
	for _, test := range tests {
		if test.FileName == filePath || test.ClassName == fcqn {
			filteredTests = append(filteredTests, test)
		}
	}
	return filteredTests
}

func matchFilesToTests(filteredTests []*tiClientTypes.TestCase, node *Node, numTestsMap map[string]int) {
	filePath := node.File

	if _, exists := numTestsMap[filePath]; exists {
		return
	}
	if len(filteredTests) == 0 {
		numTestsMap[filePath] = 1
		return
	}
	numTestsMap[filePath] = len(filteredTests)
}

func getTestStatus(filteredTests []*tiClientTypes.TestCase) types.TestState {
	numSkipped := 0
	for _, test := range filteredTests {
		if test.Result.Status == tiClientTypes.StatusFailed {
			return types.FAILURE
		}
		if test.Result.Status == tiClientTypes.StatusSkipped {
			numSkipped++
		}
	}
	if numSkipped > 0 {
		return types.UNKNOWN
	}
	return types.SUCCESS
}

func fetchFailedTests(filePath string) ([]string, error) {
	fs := filesystem.New()
	var lines []string
	_, err := os.Stat(filePath)
	if err != nil {
		return []string{}, nil
	}

	err = fs.ReadFile(filePath, func(reader io.Reader) error {
		scanner := bufio.NewScanner(reader)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line != "" { // Skip empty lines
				lines = append(lines, line)
			}
		}
		return scanner.Err()
	})

	if err != nil {
		return nil, fmt.Errorf("failed to read file %s: %w", filePath, err)
	}

	return lines, nil
}

//nolint:funlen
func CreateUploadPayload(cg *Callgraph, fileChecksums map[string]uint64, repo string, cfg *tiCfg.Cfg, commitSha string,
	reportTests []*tiClientTypes.TestCase, log *logrus.Logger, envs map[string]string) (*types.UploadCgRequest, error) {
	repoInfo := types.Identifier{
		AccountID: cfg.GetAccountID(),
		OrgID:     cfg.GetOrgID(),
		ProjectID: cfg.GetProjectID(),
		Repo:      repo,
		ExtraInfo: map[string]string{},
	}
	executionContext := instrumentation.GetTIExecutionContext(envs)
	if executionContext != nil {
		repoInfo.ExtraInfo = executionContext
	}

	var tests []types.Test
	var chains []types.Chain
	numTestsMap := make(map[string]int)
	alreadyProcessed := make(map[string]struct{})

	if cg != nil {
		nodeMap := make(map[int]Node)
		for _, node := range cg.Nodes {
			nodeMap[node.ID] = node
		}

		// Process call graph nodes to extract test information
		for _, node := range cg.Nodes {
			if node.Type == nodeTypeTest {
				var sourcePaths []string
				for _, relation := range cg.TestRelations {
					for _, testID := range relation.Tests {
						if testID == node.ID {
							if sourceNode, exists := nodeMap[relation.Source]; exists {
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
								}
							}
							break
						}
					}
				}

				uniquePaths := make(map[string]struct{})
				dedupedSourcePaths := make([]string, 0)
				for _, path := range sourcePaths {
					if _, exists := uniquePaths[path]; !exists {
						uniquePaths[path] = struct{}{}
						alreadyProcessed[path] = struct{}{}
						dedupedSourcePaths = append(dedupedSourcePaths, path)
					}
				}

				if len(dedupedSourcePaths) == 0 {
					sourcePaths = []string{}
				} else {
					sourcePaths = dedupedSourcePaths
				}
				sort.Strings(sourcePaths)

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

				if testPath == "" {
					log.Warnf("Skipping test node with empty path: node_id=%d, type=%s", node.ID, node.Type)
					continue
				}

				test := types.Test{
					Path: testPath,
					IndicativeChains: []types.IndicativeChain{
						{
							SourcePaths: sourcePaths,
						},
					},
				}
				tests = append(tests, test)

				if _, exists := fileChecksums[testPath]; !exists {
					return nil, fmt.Errorf("file checksum not found for %s", testPath)
				}
				testChecksum := fileChecksums[testPath]

				nodeCopy := node
				filteredTests := findTestsForNode(reportTests, &nodeCopy)
				chain := types.Chain{
					Path:         testPath,
					TestChecksum: strconv.FormatUint(testChecksum, 10),
					Checksum:     strconv.FormatUint(tiClientUtils.ChainChecksum(sourcePaths, fileChecksums), 10),
					State:        getTestStatus(filteredTests),
				}
				chains = append(chains, chain)
				matchFilesToTests(filteredTests, &nodeCopy, numTestsMap)
			}
		}
	}
	failedTests := []string{}
	if filePath, exists := envs["TI_FAILED_TESTS_FILE_PATH"]; exists {
		var err error
		failedTests, err = fetchFailedTests(filePath)
		if err != nil {
			log.Errorln("Failed to fetch failed tests", err)
			return nil, err
		}
	}

	// Add non-code entities to tests and chains
	nonCodeTest, nonCodeChain := instrumentation.PopulateNonCodeEntities(fileChecksums, alreadyProcessed)

	tests = append(tests, nonCodeTest)
	chains = append(chains, nonCodeChain)

	return &types.UploadCgRequest{
		Identifier:       repoInfo,
		Tests:            tests,
		Chains:           chains,
		PathToTestNumMap: numTestsMap,
		TotalTests:       len(reportTests),
		PreviousFailures: failedTests,
	}, nil
}
