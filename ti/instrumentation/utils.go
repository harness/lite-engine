package instrumentation

import (
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"strings"

	"github.com/harness/lite-engine/internal/filesystem"
	"github.com/harness/lite-engine/pipeline"
	"github.com/harness/lite-engine/ti"
	"github.com/harness/lite-engine/ti/client"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"gopkg.in/yaml.v2"
)

const (
	outDir       = "%s/ti/callgraph/" // path passed as outDir in the config.ini file
	tiConfigPath = ".ticonfig.yaml"
)

// getChangedFiles returns a list of files changed in the PR along with their corresponding status
func getChangedFiles(ctx context.Context, workspace string, log *logrus.Logger) ([]ti.File, error) {
	cmd := exec.CommandContext(ctx, gitBin, diffFilesCmd...)
	cmd.Dir = workspace
	out, err := cmd.Output()
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
			res = append(res, ti.File{Status: ti.FileDeleted, Name: t[1]})
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

	isManual := isManualExecution()
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

// createJavaAgentArg creates the ini file which is required as input to the java agent
// and returns back the path to the file.
func createJavaAgentConfigFile(runner TestRunner, packages, annotations, workspace, tmpDir string,
	fs filesystem.FileSystem, log *logrus.Logger) (string, error) {
	// Create config file
	dir := fmt.Sprintf(outDir, tmpDir)
	err := fs.MkdirAll(dir, os.ModePerm)
	if err != nil {
		log.WithError(err).Errorln(fmt.Sprintf("could not create nested directory %s", dir))
		return "", err
	}

	if packages == "" {
		pkgs, err := runner.AutoDetectPackages(workspace)
		if err != nil {
			log.WithError(err).Errorln("could not auto detect packages")
		}
		packages = strings.Join(pkgs, ",")
	}

	data := fmt.Sprintf(`outDir: %s
logLevel: 0
logConsole: false
writeTo: COVERAGE_JSON
instrPackages: %s`, dir, packages)
	// Add test annotations if they were provided
	if annotations != "" {
		data = data + "\n" + fmt.Sprintf("testAnnotations: %s", annotations)
	}

	iniFile := fmt.Sprintf("%s/config.ini", tmpDir)
	log.Infoln(fmt.Sprintf("attempting to write %s to %s", data, iniFile))
	f, err := fs.Create(iniFile)
	if err != nil {
		log.WithError(err).Errorln(fmt.Sprintf("could not create file %s", iniFile))
		return "", err
	}
	_, err = f.Write([]byte(data))
	if err != nil {
		log.WithError(err).Errorln(fmt.Sprintf("could not write %s to file %s", data, iniFile))
		return "", err
	}
	// Return path to the java agent file
	return iniFile, nil
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
		data, err = ioutil.ReadAll(r)
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

func isManualExecution() bool {
	cfg := pipeline.GetState().GetTIConfig()
	if cfg.SourceBranch == "" || cfg.TargetBranch == "" || cfg.Sha == "" {
		return true // if any of them are not set, treat as a manual execution
	}
	return false
}
