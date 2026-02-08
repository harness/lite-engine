// Copyright 2022 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package instrumentation

import (
	"context"
	"crypto/sha1" //nolint:gosec // SHA1 used for non-security purposes (unique identifier generation)
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"

	"github.com/cespare/xxhash/v2"
	"github.com/mattn/go-zglob"
	"github.com/sirupsen/logrus"
	"gopkg.in/yaml.v3"

	"github.com/harness/lite-engine/internal/filesystem"
	tiCfg "github.com/harness/lite-engine/ti/config"
	"github.com/harness/lite-engine/ti/instrumentation/csharp"
	"github.com/harness/lite-engine/ti/instrumentation/java"
	"github.com/harness/lite-engine/ti/instrumentation/python"
	"github.com/harness/lite-engine/ti/instrumentation/ruby"
	"github.com/harness/lite-engine/ti/testsplitter"
	cgTypes "github.com/harness/ti-client/chrysalis/types"
	tiClientUtils "github.com/harness/ti-client/chrysalis/utils"
	ti "github.com/harness/ti-client/types"

	tiClient "github.com/harness/ti-client/client"
)

var (
	diffFilesCmdPR   = []string{"diff", "--name-status", "--diff-filter=MADR", "HEAD@{1}", "HEAD", "-1"}
	diffFilesCmdPush = []string{"diff", "--name-status", "--diff-filter=MADR"}
	bazelCmd         = "bazel"
	execCmdCtx       = exec.CommandContext
)

const (
	gitBin       = "git"
	outDir       = "%s/ti/callgraph/" // path passed as outDir in the config.ini file
	tiConfigPath = ".ticonfig.yaml"
	// Parallelism environment variables
	harnessStepIndex      = "HARNESS_STEP_INDEX"
	harnessStepTotal      = "HARNESS_STEP_TOTAL"
	harnessStageIndex     = "HARNESS_STAGE_INDEX"
	harnessStageTotal     = "HARNESS_STAGE_TOTAL"
	ciTiRerunFailedTestFF = "CI_TI_RERUN_FAILED_TEST_FF"

	// revamp constants
	constantChecksum = 1
	NonCodeChainPath = "HARNESS_TI_NON_CODE_CHAIN_PATH"
)

// Code file extensions that are considered "code" files
var codeFileExtensions = []string{
	// Java
	".java",
	".class",
	".jar",

	// Python
	".py",
	".pyx", // Cython
	".pyi", // Type hints
	".pyc", // Python compiled files

	// Kotlin
	".kt",
	".kts", // Kotlin script

	// Scala
	".scala",
	".sc", // Scala script

	// C#
	".cs",
	".csx", // C# script

	// Ruby
	".rb",
	".rbw", // Ruby Windows

	// JavaScript
	".js",
	".mjs", // ES modules
	".jsx", // React JSX

	// TypeScript
	".ts",
	".tsx",  // React TSX
	".d.ts", // TypeScript definitions

	".ticonfig.yaml", // TI config file
}

// FindNonCodeFiles returns a deterministic string representation of all non-code file paths.
func FindNonCodeFiles(fileChecksums map[string]uint64) string {
	if len(fileChecksums) == 0 {
		return ""
	}

	nonCodePaths := make([]string, 0, len(fileChecksums))
	for filePath := range fileChecksums {
		if filePath == NonCodeChainPath {
			continue
		}

		isCodeFile := false
		for _, ext := range codeFileExtensions {
			if strings.HasSuffix(filePath, ext) {
				isCodeFile = true
				break
			}
		}

		if !isCodeFile {
			nonCodePaths = append(nonCodePaths, filePath)
		}
	}

	if len(nonCodePaths) == 0 {
		return ""
	}

	sort.Strings(nonCodePaths)
	return strings.Join(nonCodePaths, "#")
}

// PopulateNonCodeEntities builds a special test and chain entry for non-code files.
func PopulateNonCodeEntities(fileChecksums map[string]uint64, alreadyProcessed map[string]struct{}) (cgTypes.Test, cgTypes.Chain) {
	// Filter out non-code files (files that don't have code extensions)
	var nonCodePaths []string
	for filePath := range fileChecksums {
		// Check if the file has a code extension
		isCodeFile := false
		for _, ext := range codeFileExtensions {
			if strings.HasSuffix(filePath, ext) {
				isCodeFile = true
				break
			}
		}

		// If it's not a code file, add it to non-code paths
		if !isCodeFile {
			if _, exists := alreadyProcessed[filePath]; !exists {
				nonCodePaths = append(nonCodePaths, filePath)
			}
		}
	}

	// Sort paths for consistency
	sort.Strings(nonCodePaths)

	// Create the test structure
	test := cgTypes.Test{
		Path: NonCodeChainPath,
		IndicativeChains: []cgTypes.IndicativeChain{
			{
				SourcePaths: nonCodePaths,
			},
		},
	}

	chainChecksum := uint64(0)
	if len(nonCodePaths) > 0 {
		chainChecksum = tiClientUtils.ChainChecksum(nonCodePaths, fileChecksums)
	}

	chain := cgTypes.Chain{
		Path:         NonCodeChainPath,
		TestChecksum: strconv.FormatUint(fileChecksums[NonCodeChainPath], 10),
		Checksum:     strconv.FormatUint(chainChecksum, 10),
		State:        cgTypes.SUCCESS, // Default to success for non-code entities
	}

	return test, chain
}

func GetTIExecutionContext(envs map[string]string) map[string]string {
	context := make(map[string]string)

	if cgVersion, exists := envs[envHarnessTiCgVersion]; exists {
		context[envHarnessTiCgVersion] = cgVersion
	}

	if matrixValue, exists := envs[envHarnessMatrixAxis]; exists {
		if matrixAxis := readMatrixAxisString(matrixValue); matrixAxis != "" {
			context[envHarnessMatrixAxis] = matrixAxis
		}
	}

	return context
}

func readMatrixAxisString(matrixAxesEnv string) string {
	if matrixAxesEnv == "" {
		return ""
	}

	var stageMap map[string]string
	if err := json.Unmarshal([]byte(matrixAxesEnv), &stageMap); err != nil || len(stageMap) == 0 {
		return ""
	}

	// Sort keys for consistent output
	keys := make([]string, 0, len(stageMap))
	for key := range stageMap {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	// Build key=value pairs
	pairs := make([]string, 0, len(keys))
	for _, key := range keys {
		pairs = append(pairs, fmt.Sprintf("%s=%s", key, stageMap[key]))
	}

	return strings.Join(pairs, ",")
}

func getTiRunner(language, buildTool string, log *logrus.Logger, fs filesystem.FileSystem, testGlobs []string, envs map[string]string) (TestRunner, bool, error) {
	var runner TestRunner
	var useYaml bool
	switch strings.ToLower(language) {
	case "scala", "java", "kotlin":
		useYaml = false
		switch buildTool {
		case "maven":
			runner = java.NewMavenRunner(log, fs)
		case "gradle":
			runner = java.NewGradleRunner(log, fs)
		case "bazel":
			runner = java.NewBazelRunner(log, fs)
		case "sbt":
			if language != "scala" {
				return runner, useYaml, fmt.Errorf("build tool: SBT is not supported for non-Scala languages")
			}
			runner = java.NewSBTRunner(log, fs)
		default:
			return runner, useYaml, fmt.Errorf("build tool: %s is not supported for Java", buildTool)
		}
	case "csharp":
		useYaml = true
		switch buildTool {
		case "dotnet":
			runner = csharp.NewDotnetRunner(log, fs)
		case "nunitconsole":
			runner = csharp.NewNunitConsoleRunner(log, fs)
		default:
			return runner, useYaml, fmt.Errorf("could not figure out the build tool: %s", buildTool)
		}
	case "python":
		switch buildTool {
		case "pytest":
			runner = python.NewPytestRunner(log, fs, testGlobs)
		case "unittest":
			runner = python.NewUnittestRunner(log, fs, testGlobs)
		default:
			return runner, useYaml, fmt.Errorf("could not figure out the build tool: %s", buildTool)
		}
	case "ruby":
		switch buildTool {
		case "rspec":
			runner = ruby.NewRubyRunner(log, fs, testGlobs, envs)
		default:
			return runner, useYaml, fmt.Errorf("could not figure out the build tool: %s", buildTool)
		}
	default:
		return runner, useYaml, fmt.Errorf("language %s is not suported", language)
	}
	return runner, useYaml, nil
}

func GetCommitInfo(ctx context.Context, stepID string, cfg *tiCfg.Cfg) (string, error) {
	c := cfg.GetClient()
	branch := cfg.GetSourceBranch()

	resp, err := c.CommitInfo(ctx, stepID, branch)
	if err != nil {
		return "", err
	}
	return resp.LastSuccessfulCommitId, nil
}

// getTestTime gets the the timing data from TI service based on the split strategy
func getTestTime(ctx context.Context, stepID, splitStrategy string, cfg *tiCfg.Cfg) (map[string]float64, error) {
	fileTimesMap := map[string]float64{}
	if cfg == nil {
		return fileTimesMap, fmt.Errorf("TI config is not provided in setup")
	}
	c := cfg.GetClient()
	req := ti.GetTestTimesReq{}
	var res ti.GetTestTimesResp
	var err error

	switch splitStrategy {
	case testsplitter.SplitByFileTimeStr:
		req.IncludeFilename = true
		res, err = c.GetTestTimes(ctx, stepID, &req, 0)
		fileTimesMap = testsplitter.ConvertMap(res.FileTimeMap)
	case testsplitter.SplitByClassTimeStr:
		req.IncludeClassname = true
		res, err = c.GetTestTimes(ctx, stepID, &req, 0)
		fileTimesMap = testsplitter.ConvertMap(res.ClassTimeMap)
	case testsplitter.SplitByTestcaseTimeStr:
		req.IncludeTestCase = true
		res, err = c.GetTestTimes(ctx, stepID, &req, 0)
		fileTimesMap = testsplitter.ConvertMap(res.TestTimeMap)
	case testsplitter.SplitByTestSuiteTimeStr:
		req.IncludeTestSuite = true
		res, err = c.GetTestTimes(ctx, stepID, &req, 0)
		fileTimesMap = testsplitter.ConvertMap(res.SuiteTimeMap)
	case testsplitter.SplitByFileSizeStr:
		return map[string]float64{}, nil
	default:
		return map[string]float64{}, nil
	}
	if err != nil {
		return map[string]float64{}, err
	}
	return fileTimesMap, nil
}

// getSplitTests takes a list of tests as input and returns the slice of tests to run depending on
// the test split strategy and index
func getSplitTests(ctx context.Context, log *logrus.Logger, testsToSplit []ti.RunnableTest, stepID, splitStrategy string, splitIdx, splitTotal int, tiConfig *tiCfg.Cfg) ([]ti.RunnableTest, error) {
	if len(testsToSplit) == 0 {
		return testsToSplit, nil
	}

	currentTestMap := make(map[string][]ti.RunnableTest)
	currentTestSet := make(map[string]bool)
	var testID string
	for _, t := range testsToSplit {
		switch splitStrategy {
		case classTimingTestSplitStrategy, countTestSplitStrategy:
			testID = t.Pkg + "." + t.Class
		default:
			testID = t.Pkg + "." + t.Class
		}
		currentTestSet[testID] = true
		currentTestMap[testID] = append(currentTestMap[testID], t)
	}

	fileTimes := map[string]float64{}
	var err error

	// Get weights for each test depending on the strategy
	switch splitStrategy {
	case classTimingTestSplitStrategy:
		// Call TI svc to get the test timing data
		fileTimes, err = getTestTime(ctx, stepID, splitStrategy, tiConfig)
		if err != nil {
			return testsToSplit, err
		}
		log.Infoln("Successfully retrieved timing data for splitting")
	case countTestSplitStrategy:
		// Send empty fileTimesMap while processing to assign equal weights
		log.Infoln("Assigning all tests equal weight for splitting")
	default:
		// Send empty fileTimesMap while processing to assign equal weights
		log.Infoln("Assigning all tests equal weight for splitting as default strategy")
	}

	// Assign weights to the current test set if present, else average. If there are no
	// weights for taking average, set the weight as 1 to all the tests
	testsplitter.ProcessFiles(fileTimes, currentTestSet, float64(1))

	// Split tests into buckets and return tests from the current node's bucket
	testsToRun := make([]ti.RunnableTest, 0)
	buckets, _ := testsplitter.SplitFiles(fileTimes, splitTotal)
	for _, id := range buckets[splitIdx] {
		if _, ok := currentTestMap[id]; !ok {
			// This should not happen
			log.Warnln(fmt.Sprintf("Test %s from the split not present in the original set of tests, skipping", id))
			continue
		}
		testsToRun = append(testsToRun, currentTestMap[id]...)
	}
	return testsToRun, nil
}

// getChangedFilesPR returns a list of files changed with their corresponding status for a PR.
func GetChangedFilesPR(ctx context.Context, workspace string, log *logrus.Logger) ([]ti.File, error) {
	return getChangedFiles(ctx, workspace, log, diffFilesCmdPR)
}

// getChangedFilesPush returns a list of files changed with their corresponding status for push trigger/manual execution.
func GetChangedFilesPush(ctx context.Context, workspace, lastSuccessfulCommitID, currentCommitID string, log *logrus.Logger) ([]ti.File, error) {
	diffFilesCmd := diffFilesCmdPush
	diffFilesCmd = append(diffFilesCmd, lastSuccessfulCommitID, currentCommitID)
	return getChangedFiles(ctx, workspace, log, diffFilesCmd)
}

// getChangedFiles returns a list of files changed given the changed file command with their corresponding status.
func getChangedFiles(ctx context.Context, workspace string, log *logrus.Logger, diffFilesCmd []string) ([]ti.File, error) {
	cmd := exec.CommandContext(ctx, gitBin, diffFilesCmd...)
	envs := make(map[string]string)
	for _, e := range os.Environ() {
		if i := strings.Index(e, "="); i >= 0 {
			envs[e[:i]] = e[i+1:]
		}
	}
	cmd.Env = toEnv(envs)
	cmd.Dir = workspace
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, err
	}

	res := []ti.File{}

	for _, l := range strings.Split(string(out), "\n") {
		t := strings.Fields(l)
		// t looks like:
		// <M/A/D file_name> for modified/added/deleted files
		// <RXYZ old_file new_file> for renamed files where XYZ denotes %age similarity
		if len(t) == 0 {
			break
		}

		if t[0][0] == 'M' {
			res = append(res, ti.File{Status: ti.FileModified, Name: t[1]})
		} else if t[0][0] == 'A' {
			res = append(res, ti.File{Status: ti.FileAdded, Name: t[1]})
		} else if t[0][0] == 'D' {
			res = append(res, ti.File{Status: ti.FileDeleted, Name: t[1]})
		} else if t[0][0] == 'R' {
			res = append(res, ti.File{Status: ti.FileDeleted, Name: t[1]}) //nolint:gocritic
			res = append(res, ti.File{Status: ti.FileAdded, Name: t[2]})
		} else {
			// Log the error, don't error out for now
			log.WithError(err).WithField("status", t[0]).WithField("file", t[1]).Errorln("unsupported file status")
			return res, nil
		}
	}
	return res, nil
}

// addBazelFilesToChangedFiles takes a list of files and removes bazel build files and adds java files listed in target src globs
func addBazelFilesToChangedFiles(ctx context.Context, workspace string, log *logrus.Logger, oldChangedFiles []ti.File, bazelFileCountThreshold int) ([]ti.File, []string, error) {
	var moduleList []string

	// map to prevent duplicate files
	uniqueFiles := make(map[string]struct{})

	var newChangedFiles []ti.File
	// BUILD.bazel is harness-core specific, to make it generic consider changing this
	for _, file := range oldChangedFiles {
		if !strings.HasSuffix(file.Name, "BUILD.bazel") {
			// add non BUILD.bazel files to the changed files result directly and skip remainder
			if _, exists := uniqueFiles[file.Name]; !exists {
				newChangedFiles = append(newChangedFiles, file)
				uniqueFiles[file.Name] = struct{}{}
			}
			continue
		}
		// If BUILD.Bazel at the root of harness-core is modified, run all tests /... by keeping BUILD.Bazel in changed files list
		if strings.Replace(file.Name, "BUILD.bazel", "", -1) == "" {
			log.Infoln("Changed file list is: ", oldChangedFiles)
			log.Infoln("Determined to run all tests /... : ")
			return oldChangedFiles, moduleList, nil
		}
		var moduleName string
		splitName := strings.Split(file.Name, "/")
		if len(splitName) != 0 {
			moduleName = splitName[0]
		}
		allJavaSrcsOutput, countAllJavaSrcs, javaTestKindOutput, err := getJavaRulesFromBazel(ctx, file.Name, workspace)
		if err != nil {
			if javaTestKindOutput == nil {
				// if bazel queries fail then fall back to run module level tests
				moduleList = append(moduleList, moduleName)
			}
			continue
		}
		count, err := strconv.Atoi(strings.TrimSpace(string(countAllJavaSrcs)))
		if err != nil {
			count = 0
		}
		directory := filepath.Join(workspace, strings.Replace(file.Name, "/BUILD.bazel", "", -1))
		// if no srcs present in the bazel build file, then select all files under this directory as changed
		if count == 0 {
			message := fmt.Sprintf("No src detected in Bazel %v, considering all java files as changed files inside this directory %v", file.Name, directory)
			log.Infoln(message)
			newChangedFiles, err = getAllJavaFilesInsideDirectory(directory, newChangedFiles, file, uniqueFiles)
			if err != nil {
				// if failure, then add module to the list to run module level tests
				return oldChangedFiles, nil, fmt.Errorf("bazel optimazation failed %v ", err)
			}
		} else if count >= bazelFileCountThreshold {
			// if count of files changed (on bazel build modification) is over the limit set then update the module list with test target
			message := fmt.Sprintf("%v Files changed after changing bazel file %v; crosses limit set for bazelFileCount", count, file.Name)
			log.Infoln(message)
			if javaTestKindOutput != nil {
				moduleList = append(moduleList, moduleName)
			}
		} else {
			// get list of .java files from the output
			javaFileNames := extractJavaFilesFromQueryOutput(allJavaSrcsOutput)
			for _, name := range javaFileNames {
				// to prevent duplicate files
				if _, exists := uniqueFiles[name]; !exists {
					newChangedFiles = append(newChangedFiles, ti.File{Status: file.Status, Name: name})
					uniqueFiles[name] = struct{}{}
				}
			}
		}
	}
	return newChangedFiles, moduleList, nil
}

// takes bazelQueryOutput string and parses it to extract .java files and returns in a list
// sample input is "srcs = ["//module1:pkg1/pkg2/class1.java", //module1:pkg1/pkg2/class2.java"] , srcs = ["//module1:pkg1/pkg2/testclass1.java"]"
func extractJavaFilesFromQueryOutput(bazelOutput []byte) []string {
	var javaFileNames []string
	// Outer Regex to extract only srcs
	pattern1 := `srcs\s*=\s*\[[^\]]+\]`
	r1 := regexp.MustCompile(pattern1)

	sections := r1.FindAllString(string(bazelOutput), -1)
	for _, section := range sections {
		// Inner Regex to match only .java files inside each src
		pattern2 := `"//[^"]+\.java"`
		r2 := regexp.MustCompile(pattern2)
		matches := r2.FindAllString(section, -1)
		for _, match := range matches {
			srcFile := strings.Split(strings.Trim(match, `\"`), ":")
			if len(srcFile) > 1 {
				changedFile := filepath.Join(strings.TrimPrefix(srcFile[0], "//"), srcFile[1])
				javaFileNames = append(javaFileNames, changedFile)
			}
		}
	}
	return javaFileNames
}

// takes file name, runs bazel queries extract src globs defined in java rules, viz java_library and java_test
func getJavaRulesFromBazel(ctx context.Context, file, workspace string) (allSrcs, countAllSrcs, testSrcs []byte, err error) {
	bazelQueryInput := strings.Replace(file, "/BUILD.bazel", "", -1)

	// query to fetch test src globs from java rule
	// eg of the query is: bazel query 'kind("java", 980-commons:*)' --output=build  | grep 'srcs ='
	c1 := fmt.Sprintf("cd %s; %s query 'kind(\"java\", %s:*)' --output=build | grep 'srcs =' ", workspace, bazelCmd, bazelQueryInput)
	cmdArgs1 := []string{"-c", c1}
	javaKindOutput, err := execCmdCtx(ctx, "sh", cmdArgs1...).Output()
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to run bazel query %v, encountered %v ", c1, err)
	}

	// query to count the scr files in bazelQueryOutput
	// eg of the query is: bazel query 'kind("java", 332-ci-manager/app:*)' --output=build  | grep 'srcs =' | grep -o '\.java' | wc -l
	c2 := fmt.Sprintf("cd %s; %s query 'kind(\"java\", %s:*)' --output=build | grep 'srcs =' | grep -o '\\.java' | wc -l", workspace, bazelCmd, bazelQueryInput)
	cmdArgs2 := []string{"-c", c2}
	countQueryOutput, err := execCmdCtx(ctx, "sh", cmdArgs2...).Output()
	if err != nil {
		return javaKindOutput, nil, nil, fmt.Errorf("failed to run bazel query %v, encountered %v ", c1, err)
	}

	// query to get test kind for module list
	// eg of the query is: bazel query 'kind("java_library", 332-ci-manager/app:tests)' --output=build  | grep 'srcs ='
	c3 := fmt.Sprintf("cd %s; %s query 'kind(\"java_library\", %s:tests)' --output=build | grep 'srcs =' ", workspace, bazelCmd, bazelQueryInput)
	cmdArgs3 := []string{"-c", c3}
	javaTestKindOutput, err := execCmdCtx(ctx, "sh", cmdArgs3...).Output()
	if err != nil {
		return javaKindOutput, countQueryOutput, nil, fmt.Errorf("failed to run bazel query %v, encountered %v ", c1, err)
	}

	return javaKindOutput, countQueryOutput, javaTestKindOutput, err
}

// takes a directory name and adds all java files within that module/package in changed file res
func getAllJavaFilesInsideDirectory(directory string, changedFiles []ti.File, file ti.File, uniqueFiles map[string]struct{}) ([]ti.File, error) {
	err := filepath.Walk(directory, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		// add only .java files to the list
		if strings.HasSuffix(info.Name(), ".java") {
			if _, exists := uniqueFiles[path]; !exists {
				changedFiles = append(changedFiles, ti.File{Status: file.Status, Name: path})
				uniqueFiles[path] = struct{}{}
			}
		}
		return nil
	})
	return changedFiles, err
}

// selectTests takes a list of files which were changed as input and gets the tests
// to be run corresponding to that.
func SelectTests(ctx context.Context, workspace string, files []ti.File, runSelected bool, stepID string, testGlobs []string,
	fs filesystem.FileSystem, cfg *tiCfg.Cfg, rerunFailedTests bool) (ti.SelectTestsResp, error) {
	Log := logrus.New() // Revert
	Log.Infoln("Info: starting test selection")
	tiConfigYaml, err := getTiConfig(workspace, fs)
	if err != nil {
		return ti.SelectTestsResp{}, err
	}
	req := &ti.SelectTestsReq{SelectAll: !runSelected, Files: files, TiConfig: tiConfigYaml, TestGlobs: testGlobs}
	c := cfg.GetClient()
	return c.SelectTests(ctx, stepID, cfg.GetSourceBranch(), cfg.GetTargetBranch(), req, rerunFailedTests)
}

//nolint:gocritic // hugeParam: keeping struct parameter for API consistency
func filterTestsAfterSelection(selection ti.SelectTestsResp, testGlobs, excludeGlobs []string) ti.SelectTestsResp {
	if selection.SelectAll || len(testGlobs) == 0 {
		return selection
	}
	filteredTests := []ti.RunnableTest{}
	for _, test := range selection.Tests {
		if matchedAny(test.Class, testGlobs) && !matchedAny(test.Class, excludeGlobs) {
			filteredTests = append(filteredTests, test)
		}
	}
	selection.SelectedTests = len(filteredTests)
	selection.Tests = filteredTests
	return selection
}

func matchedAny(class string, globs []string) bool {
	for _, glob := range globs {
		if matchedExclude, _ := zglob.Match(glob, class); matchedExclude {
			return true
		}
	}
	return false
}

func formatTests(tests []ti.RunnableTest) string {
	testStrings := make([]string, 0)
	for _, t := range tests {
		tString := t.Class
		if t.Pkg != "" {
			tString = fmt.Sprintf("%s.", t.Pkg) + tString
		}
		if t.Autodetect.Rule != "" {
			tString += fmt.Sprintf(" %s", t.Autodetect.Rule)
		}
		testStrings = append(testStrings, tString)
	}
	return strings.Join(testStrings, ", ")
}

func DownloadFile(ctx context.Context, path, url string, fs filesystem.FileSystem, client tiClient.Client) error {
	// Create the nested directory if it doesn't exist
	dir := filepath.Dir(path)
	if err := fs.MkdirAll(dir, os.ModePerm); err != nil {
		return fmt.Errorf("could not create nested directory: %s", err)
	}
	// Create the file
	out, err := fs.Create(path)
	if err != nil {
		return err
	}
	defer out.Close()
	// Get the data
	resp, err := client.DownloadAgent(ctx, url)
	if err != nil {
		return fmt.Errorf("failed to make a request: %s", err)
	}
	defer resp.Close()

	// Writer the body to file
	_, err = io.Copy(out, resp)
	if err != nil {
		return fmt.Errorf("failed to copy: %s", err)
	}

	return nil
}

func GetV2AgentDownloadLinks(ctx context.Context, config *tiCfg.Cfg, useQAEnv bool) ([]ti.DownloadLink, error) {
	c := config.GetClient()

	buildEnv := ""
	if useQAEnv {
		buildEnv = "qa"
	}

	links, err := c.DownloadLink(ctx, "RunTestV2", runtime.GOOS, runtime.GOARCH, "", "", buildEnv)
	if err != nil {
		return links, err
	}
	return links, nil
}

// installAgents checks if the required artifacts are installed for the language
// and if not, installs them. It returns back the directory where all the agents are installed.
func installAgents(ctx context.Context, baseDir, language, os, arch, framework string,
	fs filesystem.FileSystem, log *logrus.Logger, config *tiCfg.Cfg) (string, error) {
	// Get download links from TI service
	c := config.GetClient()
	log.Infof("Getting TI agent artifact download links for language: %s", language)
	links, err := c.DownloadLink(ctx, language, os, arch, framework, "", "")
	if err != nil {
		log.WithError(err).Println("could not fetch download links for artifact download")
		return "", err
	}

	var installDir string // directory where all the agents are installed

	// Install the Artifacts
	for idx, l := range links {
		absPath := filepath.Join(baseDir, l.RelPath)
		if idx == 0 {
			installDir = filepath.Dir(absPath)
		} else if filepath.Dir(absPath) != installDir {
			return "", fmt.Errorf("artifacts don't have the same relative path: link %s and installDir %s", l, installDir)
		}
		// TODO: (Vistaar) Add check for whether the path exists here. This can be implemented
		// once we have a proper release process for agent artifacts.
		err := DownloadFile(ctx, absPath, l.URL, fs, config.GetClient())
		if err != nil {
			log.WithError(err).Printf("could not download %s to path %s\n", l.URL, installDir)
			return "", err
		}
	}

	return installDir, nil
}

// getCgDir returns Callgraph Directory
func getCgDir(tmpDir string) string {
	return fmt.Sprintf(outDir, tmpDir)
}

// createConfigFile creates the ini file which is required as input to the instrumentation agent
// and returns back the path to the file.
func createConfigFile(runner TestRunner, packages, annotations, workspace, tmpDir string,
	fs filesystem.FileSystem, log *logrus.Logger, yaml bool) (string, error) {
	// Create config file
	dir := getCgDir(tmpDir)
	err := fs.MkdirAll(dir, os.ModePerm)
	if err != nil {
		log.WithError(err).Errorln(fmt.Sprintf("could not create nested directory %s", dir))
		return "", err
	}

	if packages == "" {
		pkgs, err := runner.AutoDetectPackages(workspace) //nolint:govet
		if err != nil {
			log.WithError(err).Errorln("could not auto detect packages")
		}
		packages = strings.Join(pkgs, ",")
	}
	var data string
	var outputFile string

	// TODO: Create a struct for this once all languages use YAML input
	if !yaml {
		outputFile = fmt.Sprintf("%s/config.ini", tmpDir)
		data = fmt.Sprintf(`outDir: %s
logLevel: 0
logConsole: false
writeTo: COVERAGE_JSON
instrPackages: %s`, dir, packages)
	} else {
		outputFile = fmt.Sprintf("%s/config.yaml", tmpDir)
		p := strings.Split(packages, ",")
		for idx, s := range p {
			p[idx] = fmt.Sprintf("'%s'", s)
		}
		data = fmt.Sprintf(`outDir: '%s'
logLevel: 0
writeTo: [COVERAGE_JSON]
instrPackages: [%s]`, dir, strings.Join(p, ","))
	}

	// Add test annotations if they were provided
	if annotations != "" {
		data = data + "\n" + fmt.Sprintf("testAnnotations: %s", annotations)
	}

	log.Infof("Attempting to write to %s with config:\n%s", outputFile, data)
	f, err := fs.Create(outputFile)
	if err != nil {
		log.WithError(err).Errorln(fmt.Sprintf("could not create file %s", outputFile))
		return "", err
	}
	_, err = f.WriteString(data)
	defer f.Close()
	if err != nil {
		log.WithError(err).Errorln(fmt.Sprintf("could not write %s to file %s", data, outputFile))
		return "", err
	}
	// Return path to the config.ini file
	return outputFile, nil
}

func getTiConfig(workspace string, fs filesystem.FileSystem) (ti.TiConfig, error) {
	res := ti.TiConfig{}

	path := fmt.Sprintf("%s/%s", workspace, tiConfigPath)
	_, err := os.Stat(path)
	if os.IsNotExist(err) {
		return res, nil
	}
	var data []byte
	err = fs.ReadFile(path, func(r io.Reader) error {
		data, err = io.ReadAll(r)
		return err
	})
	if err != nil {
		return res, fmt.Errorf("could not read ticonfig file: %w", err)
	}
	err = yaml.Unmarshal(data, &res)
	if err != nil {
		return res, fmt.Errorf("could not unmarshal ticonfig file: %w", err)
	}
	return res, nil
}

func valid(tests []ti.RunnableTest) bool {
	for _, t := range tests {
		if t.Class == "" {
			return false
		}
	}
	return true
}

func IsManualExecution(cfg *tiCfg.Cfg) bool {
	if cfg.GetSourceBranch() == "" || cfg.GetTargetBranch() == "" || cfg.GetSha() == "" {
		return true // if any of them are not set, treat as a manual execution
	}
	return false
}

func IsPushTriggerExecution(cfg *tiCfg.Cfg) bool {
	if (cfg.GetSourceBranch() == cfg.GetTargetBranch()) && !IsManualExecution(cfg) {
		return true
	}
	return false
}

// helper function that converts a key value map of
// environment variables to a string slice in key=value
// format.
func toEnv(env map[string]string) []string {
	var envs []string
	for k, v := range env {
		if v != "" {
			envs = append(envs, k+"="+v)
		}
	}
	return envs
}

// GetUniqueHash generates a unique 4-character hash from the step ID and TI config.
// It combines accountID, projectID, pipelineID, stageID and step ID to create a unique identifier.
func GetUniqueHash(stepID string, cfg *tiCfg.Cfg) string {
	uniqueID := cfg.GetAccountID() + "_" + cfg.GetPipelineID() + "_" + cfg.GetStageID() + "_" + cfg.GetBuildID() + "_" + stepID
	hasher := sha1.New() //nolint:gosec // SHA1 used for non-security purposes (unique identifier generation)
	hasher.Write([]byte(uniqueID))
	fullHash := hex.EncodeToString(hasher.Sum(nil))
	return fmt.Sprintf("s%x", fullHash[:8])
}

func GetStepStrategyIteration(envs map[string]string) (int, error) {
	idxStr, ok := envs[harnessStepIndex]
	if !ok {
		return -1, fmt.Errorf("parallelism strategy iteration variable not set %s", harnessStepIndex)
	}
	idx, err := strconv.Atoi(idxStr)
	if err != nil {
		return -1, fmt.Errorf("unable to convert %s from string to int", harnessStepIndex)
	}
	return idx, nil
}

func GetStepStrategyIterations(envs map[string]string) (int, error) {
	totalStr, ok := envs[harnessStepTotal]
	if !ok {
		return -1, fmt.Errorf("parallelism total iteration variable not set %s", harnessStepTotal)
	}
	total, err := strconv.Atoi(totalStr)
	if err != nil {
		return -1, fmt.Errorf("unable to convert %s from string to int", harnessStepTotal)
	}
	return total, nil
}

func GetStageStrategyIteration(envs map[string]string) (int, error) {
	idxStr, ok := envs[harnessStageIndex]
	if !ok {
		return -1, fmt.Errorf("parallelism strategy iteration variable not set %s", harnessStageIndex)
	}
	idx, err := strconv.Atoi(idxStr)
	if err != nil {
		return -1, fmt.Errorf("unable to convert %s from string to int", harnessStageIndex)
	}
	return idx, nil
}

func GetStageStrategyIterations(envs map[string]string) (int, error) {
	totalStr, ok := envs[harnessStageTotal]
	if !ok {
		return -1, fmt.Errorf("parallelism total iteration variable not set %s", harnessStageTotal)
	}
	total, err := strconv.Atoi(totalStr)
	if err != nil {
		return -1, fmt.Errorf("unable to convert %s from string to int", harnessStageTotal)
	}
	return total, nil
}

func IsStepParallelismEnabled(envs map[string]string) bool {
	v1, err1 := GetStepStrategyIteration(envs)
	v2, err2 := GetStepStrategyIterations(envs)
	if err1 != nil || err2 != nil || v1 >= v2 || v2 <= 1 {
		return false
	}
	return true
}

func IsStageParallelismEnabled(envs map[string]string) bool {
	v1, err1 := GetStageStrategyIteration(envs)
	v2, err2 := GetStageStrategyIterations(envs)
	if err1 != nil || err2 != nil || v1 >= v2 || v2 <= 1 {
		return false
	}
	return true
}

func IsParallelismEnabled(envs map[string]string) bool {
	return IsStepParallelismEnabled(envs) || IsStageParallelismEnabled(envs)
}

// GetSplitIdxAndTotal returns splitIdx and SplitTotal based on step envs
func GetSplitIdxAndTotal(envs map[string]string) (splitIdx, splitTotal int) {
	stepIdx, _ := GetStepStrategyIteration(envs)
	stepTotal, _ := GetStepStrategyIterations(envs)
	if !IsStepParallelismEnabled(envs) {
		stepIdx = 0
		stepTotal = 1
	}
	stageIdx, _ := GetStageStrategyIteration(envs)
	stageTotal, _ := GetStageStrategyIterations(envs)
	if !IsStageParallelismEnabled(envs) {
		stageIdx = 0
		stageTotal = 1
	}
	splitIdx = stepTotal*stageIdx + stepIdx
	splitTotal = stepTotal * stageTotal
	return splitIdx, splitTotal
}

// GetSplitIdxAndTotalWithMatrix returns splitIdx and SplitTotal based on step envs considering matrix
func GetSplitIdxAndTotalWithMatrix(envs map[string]string) (splitIdx, splitTotal int) {
	stepIdx, _ := GetStepStrategyIteration(envs)
	stepTotal, _ := GetStepStrategyIterations(envs)
	if !IsStepParallelismEnabled(envs) {
		stepIdx = 0
		stepTotal = 1
	}
	stageIdx, _ := GetStageStrategyIteration(envs)
	stageTotal, _ := GetStageStrategyIterations(envs)
	if !IsStageParallelismEnabled(envs) || IsMatrixEnabledStage(envs) {
		stageIdx = 0
		stageTotal = 1
	}

	splitIdx = stepTotal*stageIdx + stepIdx
	splitTotal = stepTotal * stageTotal
	return splitIdx, splitTotal
}

func IsMatrixEnabledStage(envs map[string]string) bool {
	if matrixEnvVar, exists := envs[envHarnessMatrixAxis]; exists {
		var stageMap map[string]string
		if err := json.Unmarshal([]byte(matrixEnvVar), &stageMap); err == nil && len(stageMap) > 0 {
			return true
		}
	}
	return false
}

func IsStageParallelismEnabledWithoutMatrix(envs map[string]string) bool {
	return IsStageParallelismEnabled(envs) && !IsMatrixEnabledStage(envs)
}

// GetGitFileChecksums gets git file checksums from the specified repository
// and returns them as a map of filepath to 64-bit checksum
func GetGitFileChecksums(ctx context.Context, repoDir string, log *logrus.Logger) (map[string]uint64, error) {
	// Git ls-tree output format: "<mode> <type> <checksum>\t<filepath>"
	// Minimum required parts: mode, type, checksum, filepath
	const minGitLsTreeParts = 4
	// Git checksums are 160-bit (40 hex chars), we need at least 16 chars for uint64 conversion
	const minChecksumLength = 16

	log.Infof("Getting git file checksums from directory: %s", repoDir)

	// Execute git ls-tree -r HEAD . command in the specified directory
	cmd := execCmdCtx(ctx, gitBin, "ls-tree", "-r", "HEAD", ".")
	cmd.Dir = repoDir
	tiConfig, err := getTiConfig(repoDir, filesystem.New())
	if err != nil {
		return nil, fmt.Errorf("failed to get ti config: %w", err)
	}
	ignoreList := tiConfig.Config.Ignore

	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to execute git ls-tree command: %w", err)
	}

	// Parse the output and create file:checksum map
	fileChecksums := make(map[string]uint64)
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")

	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}

		// Git ls-tree output format: "<mode> <type> <checksum>\t<filepath>"
		// Example: "100644 blob a1b2c3d4e5f6... path/to/file.txt"
		parts := strings.Fields(line)
		if len(parts) < minGitLsTreeParts {
			log.Warnf("Skipping malformed git ls-tree line: %s", line)
			continue
		}

		// Extract checksum (3rd field) and filepath (4th field onwards, joined with spaces)
		fullChecksum := parts[2]
		filepath := strings.Join(parts[3:], " ")

		// When a file is in ignore list, we will assign a constant checksum to it so any change is never detected for the file.
		if isFileInIgnoreList(filepath, ignoreList) {
			fileChecksums[filepath] = constantChecksum
			continue
		}

		// Take first 16 characters of 160-bit checksum and convert to uint64
		if len(fullChecksum) < minChecksumLength {
			log.Warnf("Skipping file with short checksum: %s (checksum: %s)", filepath, fullChecksum)
			continue
		}

		checksum64, err := strconv.ParseUint(fullChecksum[:minChecksumLength], 16, 64)
		if err != nil {
			log.Warnf("Failed to parse checksum for file %s: %v", filepath, err)
			continue
		}

		fileChecksums[filepath] = checksum64
	}
	nonCodeChecksumStr := FindNonCodeFiles(fileChecksums)
	fileChecksums[NonCodeChainPath] = xxhash.Sum64String(nonCodeChecksumStr)
	log.Infof("Successfully processed %d files from git repository", len(fileChecksums))
	return fileChecksums, nil
}

func isFileInIgnoreList(filePath string, ignoreList []string) bool {
	for _, ignore := range ignoreList {
		matched, _ := zglob.Match(ignore, filePath)
		if matched {
			return true
		}
	}
	return false
}
