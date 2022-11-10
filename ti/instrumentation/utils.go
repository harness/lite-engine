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
	"strconv"
	"strings"

	"github.com/harness/lite-engine/internal/filesystem"
	"github.com/harness/lite-engine/pipeline"
	"github.com/harness/lite-engine/ti"
	"github.com/harness/lite-engine/ti/client"
	"github.com/harness/lite-engine/ti/testsplitter"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"gopkg.in/yaml.v2"
)

var (
	diffFilesCmd = []string{"diff", "--name-status", "--diff-filter=MADR", "HEAD@{1}", "HEAD", "-1"}
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

func getTestTime(ctx context.Context, splitStrategy string) (map[string]float64, error) {
	fileTimesMap := map[string]float64{}
	// Get the timing data
	cfg := pipeline.GetState().GetTIConfig()
	if cfg == nil || cfg.URL == "" {
		return fileTimesMap, fmt.Errorf("TI config is not provided in setup")
	}
	c := client.NewHTTPClient(cfg.URL, cfg.Token, cfg.AccountID, cfg.OrgID, cfg.ProjectID,
		cfg.PipelineID, cfg.BuildID, cfg.StageID, cfg.Repo, cfg.Sha, false)

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
func getSplitTests(ctx context.Context, log *logrus.Logger, testsToSplit []ti.RunnableTest, splitStrategy string, splitIdx, splitTotal int) ([]ti.RunnableTest, error) {
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
		fileTimes, err = getTestTime(ctx, splitStrategy)
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

// getChangedFiles returns a list of files changed in the PR along with their corresponding status
func getChangedFiles(ctx context.Context, workspace string, log *logrus.Logger) ([]ti.File, error) {
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

// selectTests takes a list of files which were changed as input and gets the tests
// to be run corresponding to that.
func selectTests(ctx context.Context, workspace string, files []ti.File, runSelected bool, stepID string,
	fs filesystem.FileSystem) (ti.SelectTestsResp, error) {
	config := pipeline.GetState().GetTIConfig()
	if config == nil || config.URL == "" {
		return ti.SelectTestsResp{}, fmt.Errorf("TI config is not provided in setup")
	}

	isManual := IsManualExecution()
	source := config.SourceBranch
	if source == "" && !isManual {
		return ti.SelectTestsResp{}, fmt.Errorf("source branch is not set")
	}
	target := config.TargetBranch
	if target == "" && !isManual {
		return ti.SelectTestsResp{}, fmt.Errorf("target branch is not set")
	} else if isManual {
		target = config.CommitBranch
		if target == "" {
			return ti.SelectTestsResp{}, fmt.Errorf("commit branch is not set")
		}
	}

	ticonfig, err := getTiConfig(workspace, fs)
	if err != nil {
		return ti.SelectTestsResp{}, err
	}

	req := &ti.SelectTestsReq{SelectAll: !runSelected, Files: files, TiConfig: ticonfig}

	c := client.NewHTTPClient(config.URL, config.Token, config.AccountID, config.OrgID, config.ProjectID,
		config.PipelineID, config.BuildID, config.StageID, config.Repo, config.Sha, false)
	return c.SelectTests(ctx, stepID, source, target, req)
}

func formatTests(tests []ti.RunnableTest) string {
	testStrings := make([]string, 0)
	for _, t := range tests {
		tString := fmt.Sprintf("%s.%s", t.Pkg, t.Class)
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
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// Check server response
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("bad status: %s", resp.Status)
	}

	// Writer the body to file
	_, err = io.Copy(out, resp.Body)
	if err != nil {
		return err
	}

	return nil
}

// installAgents checks if the required artifacts are installed for the language
// and if not, installs them. It returns back the directory where all the agents are installed.
func installAgents(ctx context.Context, baseDir, language, os, arch, framework string,
	fs filesystem.FileSystem, log *logrus.Logger) (string, error) {
	config := pipeline.GetState().GetTIConfig()

	c := client.NewHTTPClient(config.URL, config.Token, config.AccountID, config.OrgID, config.ProjectID,
		config.PipelineID, config.BuildID, config.StageID, config.Repo, config.Sha, false)
	log.Infoln("getting TI agent artifact download links")
	links, err := c.DownloadLink(ctx, language, os, arch, framework)
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

// createConfigFile creates the ini file which is required as input to the instrumentation agent
// and returns back the path to the file.
func createConfigFile(runner TestRunner, packages, annotations, workspace, tmpDir string,
	fs filesystem.FileSystem, log *logrus.Logger, yaml bool) (string, error) {
	// Create config file
	dir := fmt.Sprintf(outDir, tmpDir)
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

	log.Infoln(fmt.Sprintf("attempting to write %s to %s", data, outputFile))
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

func getTiConfig(workspace string, fs filesystem.FileSystem) (ti.Config, error) {
	res := ti.Config{}

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

func IsManualExecution() bool {
	cfg := pipeline.GetState().GetTIConfig()
	if cfg.SourceBranch == "" || cfg.TargetBranch == "" || cfg.Sha == "" {
		return true // if any of them are not set, treat as a manual execution
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
