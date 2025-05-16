package maven

import (
	"crypto/sha1" // #nosec G505
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/harness/ti-client/types"
	"github.com/harness/ti-client/types/cache/maven"
	"github.com/mattn/go-zglob"
	"github.com/sirupsen/logrus"
)

const (
	mavenReportPathRegex = "/harness/**/target/maven-incremental/*.xml"
	tmpFilePath          = "/tmp/maven-cache-marker"
	markerFilePerm       = 0600
	markerDirPerm        = 0755
)

func ParseSavings(workspace string, log *logrus.Logger) (types.IntelligenceExecutionState, []maven.CacheReport, error) {
	cacheState := types.FULL_RUN
	var reports []maven.CacheReport

	fmt.Println("Parsing Maven cache savings...")
	// Find all XML files in any maven-incremental directory recursively using mavenReportPathRegex
	files, err := zglob.Glob(mavenReportPathRegex)
	if err != nil {
		return cacheState, nil, fmt.Errorf("failed to find cache reports: %w", err)
	}

	if len(files) == 0 {
		return cacheState, nil, fmt.Errorf("no cache reports found")
	}

	processedFiles := 0

	for _, file := range files {
		markerPath := markerFilePath(tmpFilePath, file)
		if markerExists(markerPath) {
			continue
		}

		report, err := parseXMLReport(file)
		if err != nil {
			log.WithError(err).WithField("file", file).Errorln("failed to parse cache report")
			continue
		}

		reports = append(reports, *report)

		if markerErr := createMarkerFile(markerPath, file); markerErr != nil {
			log.Printf("failed to create marker file %s: %v", markerPath, markerErr)
		}

		// Check if any project in this report is cached
		for _, project := range report.Projects {
			if project.ChecksumMatched && project.LifecycleMatched && project.Source == "REMOTE" {
				cacheState = types.OPTIMIZED
				break
			}
		}
		processedFiles++
	}

	if processedFiles == 0 {
		return types.DISABLED, nil, fmt.Errorf("no cache reports found for maven")
	}

	fmt.Println("Parsed Maven cache savings with state:", cacheState)
	return cacheState, reports, nil
}

func parseXMLReport(filePath string) (*maven.CacheReport, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	content, err := io.ReadAll(file)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	var report maven.CacheReport
	if err := xml.Unmarshal(content, &report); err != nil {
		return nil, fmt.Errorf("failed to parse XML: %w", err)
	}

	return &report, nil
}

// markerFilePath generates a marker file path for a given report file
func markerFilePath(tmpFilePath, file string) string {
	h := sha1.New() // #nosec G401
	h.Write([]byte(file))
	markerName := hex.EncodeToString(h.Sum(nil)) + ".parsed"
	return filepath.Join(tmpFilePath, markerName)
}

// markerExists checks if a marker file exists
func markerExists(markerPath string) bool {
	_, err := os.Stat(markerPath)
	return err == nil
}

// createMarkerFile creates a marker file with the original file path as content
func createMarkerFile(markerPath, file string) error {
	// Ensure the directory exists
	if err := os.MkdirAll(filepath.Dir(markerPath), markerDirPerm); err != nil {
		return fmt.Errorf("failed to create marker directory: %w", err)
	}
	return os.WriteFile(markerPath, []byte(file), markerFilePerm)
}
