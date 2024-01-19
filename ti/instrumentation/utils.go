// Copyright 2022 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package instrumentation

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/harness/lite-engine/internal/filesystem"
	tiCfg "github.com/harness/lite-engine/ti/config"
	"github.com/harness/lite-engine/ti/instrumentation/csharp"
	"github.com/harness/lite-engine/ti/instrumentation/java"
	"github.com/harness/lite-engine/ti/instrumentation/python"
	"github.com/harness/lite-engine/ti/instrumentation/ruby"
	"github.com/harness/lite-engine/ti/testsplitter"
	ti "github.com/harness/ti-client/types"
	"github.com/mattn/go-zglob"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"gopkg.in/yaml.v2"
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
	harnessStepIndex  = "HARNESS_STEP_INDEX"
	harnessStepTotal  = "HARNESS_STEP_TOTAL"
	harnessStageIndex = "HARNESS_STAGE_INDEX"
	harnessStageTotal = "HARNESS_STAGE_TOTAL"
)

func getTiRunner(language, buildTool string, log *logrus.Logger, fs filesystem.FileSystem, testGlobs []string) (TestRunner, bool, error) {
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
			runner = ruby.NewRubyRunner(log, fs)
		default:
			return runner, useYaml, fmt.Errorf("could not figure out the build tool: %s", buildTool)
		}
	default:
		return runner, useYaml, fmt.Errorf("language %s is not suported", language)
	}
	return runner, useYaml, nil
}

func getCommitInfo(ctx context.Context, stepID string, cfg *tiCfg.Cfg) (string, error) {
	c := cfg.GetClient()
	branch := cfg.GetSourceBranch()

	resp, err := c.CommitInfo(ctx, stepID, branch)
	if err != nil {
		return "", err
	}
	return resp.LastSuccessfulCommitId, nil
}

// getTestTime gets the the timing data from TI service based on the split strategy
func getTestTime(ctx context.Context, splitStrategy string, cfg *tiCfg.Cfg) (map[string]float64, error) {
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
		res, err = c.GetTestTimes(ctx, &req)
		fileTimesMap = testsplitter.ConvertMap(res.FileTimeMap)
	case testsplitter.SplitByClassTimeStr:
		req.IncludeClassname = true
		res, err = c.GetTestTimes(ctx, &req)
		fileTimesMap = testsplitter.ConvertMap(res.ClassTimeMap)
	case testsplitter.SplitByTestcaseTimeStr:
		req.IncludeTestCase = true
		res, err = c.GetTestTimes(ctx, &req)
		fileTimesMap = testsplitter.ConvertMap(res.TestTimeMap)
	case testsplitter.SplitByTestSuiteTimeStr:
		req.IncludeTestSuite = true
		res, err = c.GetTestTimes(ctx, &req)
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
func getSplitTests(ctx context.Context, log *logrus.Logger, testsToSplit []ti.RunnableTest, splitStrategy string, splitIdx, splitTotal int, tiConfig *tiCfg.Cfg) ([]ti.RunnableTest, error) {
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
		fileTimes, err = getTestTime(ctx, splitStrategy, tiConfig)
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
func getChangedFilesPR(ctx context.Context, workspace string, log *logrus.Logger) ([]ti.File, error) {
	return getChangedFiles(ctx, workspace, log, diffFilesCmdPR)
}

// getChangedFilesPush returns a list of files changed with their corresponding status for push trigger/manual execution.
func getChangedFilesPush(ctx context.Context, workspace, lastSuccessfulCommitID, currentCommitID string, log *logrus.Logger) ([]ti.File, error) {
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
func addBazelFilesToChangedFiles(ctx context.Context, workspace string, log *logrus.Logger, res []ti.File, bazelFileCount string) ([]ti.File, []string, error) {
	var moduleList []string
	threshold, err := strconv.Atoi(bazelFileCount)
	if err != nil {
		return res, moduleList, fmt.Errorf("bazelFileCount not set correctly in ticonfig.yml, expecting number found character %v ", bazelFileCount)
	}

	//map to prevent duplicate files
	uniqueFiles := make(map[string]struct{})

	var changedRes []ti.File
	for _, file := range res {
		if !strings.HasSuffix(string(file.Name), "BUILD.bazel") {
			//add non BUILD.bazel files to the changed files result directly and skip remainder
			if _, exists := uniqueFiles[file.Name]; !exists {
				changedRes = append(changedRes, file)
				uniqueFiles[file.Name] = struct{}{}
			}
			continue
		}
		//If BUILD.Bazel at the root of harness-core is modified, run all tests /... by keeping BUILD.Bazel in changed files list
		if len(strings.Replace(file.Name, "BUILD.bazel", "", -1)) == 0 {
			log.Infoln("Changed file list is: ", res)
			log.Infoln("Determined to run all tests /... : ")
			return res, moduleList, nil
		}
		allJavaSrcsOutput, countAllJavaSrcs, javaTestKindOutput, err := getJavaRulesFromBazel(file.Name, workspace, ctx)
		if err != nil {
			fmt.Errorf("failed to run bazel query, error encountered %v: ", err)
		}

		count, err := strconv.Atoi(strings.TrimSpace(string(countAllJavaSrcs)))
		if err != nil {
			count = 0
		}

		directory := filepath.Join(workspace, "/", strings.Replace(file.Name, "/BUILD.bazel", "", -1))
		//if no srcs present in the bazel build file, then select all files under this directory as changed
		if count == 0 {
			message := fmt.Sprintf("No src detected in Bazel %v, selecting all java files as changed files inside the directory %v", file.Name, directory)
			log.Infoln(message)
			changedRes, err = getAllJavaFilesInsideDirectory(directory, changedRes, file, uniqueFiles)
		} else if count >= threshold {
			//if count of files changed due to bazel build file modification is over the limit set then update the module list with test target
			message := fmt.Sprintf("%v Files changed after changing bazel file %v; crosses limit set for bazelFileCount", count, file.Name)
			log.Infoln(message)
			if javaTestKindOutput != nil {
				splitName := strings.Split(file.Name, "/")
				moduleName := splitName[0]
				moduleList = append(moduleList, moduleName)
			}
		} else {
			//get list of .java files from the output
			javaFileNames := extractJavaFilesFromQueryOutput(allJavaSrcsOutput)
			for _, name := range javaFileNames {
				//to prevent duplicate files
				if _, exists := uniqueFiles[name]; !exists {
					changedRes = append(changedRes, ti.File{Status: file.Status, Name: name})
					uniqueFiles[name] = struct{}{}
				}
			}
		}
	}
	return changedRes, moduleList, nil
}

// takes bazelQueryOutput string and parses it to extract .java files and returns in a list
func extractJavaFilesFromQueryOutput(bazelOutput []byte) []string {
	var javaFileNames []string
	// Outer Regex to extract only srcs
	pattern1 := `srcs = \[[^\]]+\]`
	r1 := regexp.MustCompile(pattern1)

	sections := r1.FindAllString(string(bazelOutput), -1)
	for _, section := range sections {
		// Inner Regex to match only .java files inside each src
		pattern2 := `"//[^"]+\.java"`
		r2 := regexp.MustCompile(pattern2)
		matches := r2.FindAllString(section, -1)
		for _, match := range matches {
			srcFile := strings.Split(strings.Trim(match, `\"`), ":")
			changedFile := strings.TrimLeft(string(srcFile[0]), "//") + "/" + string(srcFile[1])
			javaFileNames = append(javaFileNames, changedFile)
		}
	}
	return javaFileNames
}

// takes file name, runs bazel queries extract src globs defined in java rules, viz java_library and java_test
func getJavaRulesFromBazel(file string, workspace string, ctx context.Context) ([]byte, []byte, []byte, error) {
	bazelQueryInput := strings.Replace(file, "/BUILD.bazel", "", -1)

	//query to fetch test src globs from java rule
	//eg of the query is: bazel query 'kind("java", 980-commons:*)' --output=build  | grep 'srcs ='
	c1 := fmt.Sprintf("cd %s; %s query 'kind(\"java\", %s:*)' --output=build | grep 'srcs =' ", workspace, bazelCmd, bazelQueryInput)
	cmdArgs1 := []string{"-c", c1}
	javaKindOutput, err := execCmdCtx(ctx, "sh", cmdArgs1...).Output()
	if err != nil {
		fmt.Errorf("failed to run bazel query %v, encountered %v as %v has no java rule", c1, err, file)
	}

	//query to count the scr files in bazelQueryOutput
	//eg of the query is: bazel query 'kind("java", 332-ci-manager/app:*)' --output=build  | grep 'srcs =' | grep -o '\.java' | wc -l
	c2 := fmt.Sprintf("cd %s; %s query 'kind(\"java\", %s:*)' --output=build | grep 'srcs =' | grep -o '\\.java' | wc -l", workspace, bazelCmd, bazelQueryInput)
	cmdArgs2 := []string{"-c", c2}
	countQueryOutput, err := execCmdCtx(ctx, "sh", cmdArgs2...).Output()

	//query to get test kind for module list
	//eg of the query is: bazel query 'kind("java_library", 332-ci-manager/app:tests)' --output=build  | grep 'srcs ='
	c3 := fmt.Sprintf("cd %s; %s query 'kind(\"java_library\", %s:tests)' --output=build | grep 'srcs =' ", workspace, bazelCmd, bazelQueryInput)
	cmdArgs3 := []string{"-c", c3}
	javaTestKindOutput, err := execCmdCtx(ctx, "sh", cmdArgs3...).Output()

	return javaKindOutput, countQueryOutput, javaTestKindOutput, err
}

// takes a directory name and adds all java files within that module/package in changed file res
func getAllJavaFilesInsideDirectory(directory string, res []ti.File, file ti.File, uniqueFiles map[string]struct{}) ([]ti.File, error) {
	err := filepath.Walk(directory, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		// add only .java files to the list
		if strings.HasSuffix(info.Name(), ".java") {
			if _, exists := uniqueFiles[path]; !exists {
				res = append(res, ti.File{Status: file.Status, Name: path})
				uniqueFiles[path] = struct{}{}
			}
		}
		return nil
	})
	return res, err
}

// selectTests takes a list of files which were changed as input and gets the tests
// to be run corresponding to that.
func selectTests(ctx context.Context, workspace string, files []ti.File, runSelected bool, stepID string,
	fs filesystem.FileSystem, cfg *tiCfg.Cfg) (ti.SelectTestsResp, error) {
	tiConfigYaml, err := getTiConfig(workspace, fs)
	if err != nil {
		return ti.SelectTestsResp{}, err
	}
	req := &ti.SelectTestsReq{SelectAll: !runSelected, Files: files, TiConfig: tiConfigYaml}
	c := cfg.GetClient()
	return c.SelectTests(ctx, stepID, cfg.GetSourceBranch(), cfg.GetTargetBranch(), req)
}

func filterTestsAfterSelection(selection ti.SelectTestsResp, testGlobs string) ti.SelectTestsResp {
	if selection.SelectAll || testGlobs == "" {
		return selection
	}
	testGlobSlice := strings.Split(testGlobs, ",")
	filteredTests := []ti.RunnableTest{}
	for _, test := range selection.Tests {
		for _, glob := range testGlobSlice {
			if matched, _ := zglob.Match(glob, test.Class); matched {
				filteredTests = append(filteredTests, test)
				break
			}
		}
	}
	selection.SelectedTests = len(filteredTests)
	selection.Tests = filteredTests
	return selection
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

func downloadFile(ctx context.Context, path, url string, fs filesystem.FileSystem) error {
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
	req, err := http.NewRequestWithContext(ctx, "GET", url, http.NoBody)
	if err != nil {
		return fmt.Errorf("failed to create request with context: %s", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to make a request: %s", err)
	}
	defer resp.Body.Close()

	// Check server response
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("bad status: %s", resp.Status)
	}

	// Writer the body to file
	_, err = io.Copy(out, resp.Body)
	if err != nil {
		return fmt.Errorf("failed to copy: %s", err)
	}

	return nil
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
		err := downloadFile(ctx, absPath, l.URL, fs)
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
		return res, errors.Wrap(err, "could not read ticonfig file")
	}
	err = yaml.Unmarshal(data, &res)
	if err != nil {
		return res, errors.Wrap(err, "could not unmarshal ticonfig file")
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
