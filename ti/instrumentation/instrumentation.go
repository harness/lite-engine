package instrumentation

import (
	"context"
	"os/exec"
	"strings"

	"github.com/harness/lite-engine/ti"
	"github.com/sirupsen/logrus"
)

var (
	gitBin       = "git"
	diffFilesCmd = []string{"diff", "--name-status", "--diff-filter=MADR", "HEAD@{1}", "HEAD", "-1"}
)

// GetChangedFiles returns a list of files changed in the PR along with their corresponding status
func GetChangedFiles(ctx context.Context, workspace string) ([]ti.File, error) {
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
			logrus.WithError(err).WithField("status", t[0]).WithField("file", t[1]).Errorln("unsupported file status")
			return res, nil
		}
	}
	return res, nil
}
