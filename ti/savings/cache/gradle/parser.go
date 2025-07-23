package gradle

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/harness/ti-client/types"
	gradleTypes "github.com/harness/ti-client/types/cache/gradle"
	"github.com/mattn/go-zglob"
	"github.com/sirupsen/logrus"
	"golang.org/x/net/html"
)

const (
	gradleProfilePathRegex = "build/reports/profile/*.html"
)

func ParseSavings(workspace string, log *logrus.Logger) (types.IntelligenceExecutionState, []gradleTypes.Profile, int, error) {
	cacheState := types.DISABLED
	profiles := make([]gradleTypes.Profile, 0)
	totalBuildTime := 0

	files, err := getProfileFiles(workspace, log)
	if err != nil {
		return cacheState, profiles, totalBuildTime, err
	}
	for _, file := range files {
		htmlNode, err := readHTMLFromFile(file)
		if err != nil {
			continue
		}
		profile, cached, err := parseProfileFromHtml(htmlNode)
		if err == nil {
			totalBuildTime += int(profile.BuildTimeMs)
			if cached {
				cacheState = types.OPTIMIZED
			} else {
				cacheState = types.FULL_RUN
			}
			profiles = append(profiles, profile)
		}
	}
	if len(profiles) == 0 {
		return cacheState, profiles, totalBuildTime, errors.New("no valid gradle profile found")
	}
	return cacheState, profiles, totalBuildTime, nil
}

func readHTMLFromFile(filePath string) (*html.Node, error) {
	// Append ".processed" to the file path
	processedFilePath := filePath + ".processed"

	// Rename the file to append ".processed". This ensures the original file is not processed multiple times
	err := os.Rename(filePath, processedFilePath)
	if err != nil {
		return nil, err
	}

	// Open the renamed file
	file, err := os.Open(processedFilePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()                 // Ensure the file gets closed when the function exits
	defer os.Remove(processedFilePath) // Ensure the file gets cleanup up when the function exits

	content, err := io.ReadAll(file)
	if err != nil {
		return nil, err
	}

	// Parse the content as HTML
	reader := strings.NewReader(string(content))
	doc, err := html.Parse(reader)
	if err != nil {
		return nil, err
	}

	return doc, nil
}

func getProfileFiles(workspace string, log *logrus.Logger) ([]string, error) {
	path := fmt.Sprintf("%s/%s", workspace, gradleProfilePathRegex)
	files, err := zglob.Glob(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return files, fmt.Errorf("no profiles present")
		}
		log.WithError(err).WithField("path", path).
			Errorln("errored while trying to resolve path regex for profiles")
		return files, err
	}
	if len(files) == 0 {
		return files, fmt.Errorf("no profiles present")
	}
	return files, nil
}
