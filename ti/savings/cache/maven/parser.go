package maven

import (
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/harness/ti-client/types"
	"github.com/harness/ti-client/types/cache/maven"
	"github.com/sirupsen/logrus"
)

func ParseSavings(workspace string, log *logrus.Logger) (types.IntelligenceExecutionState, []maven.CacheReport, error) {
	cacheState := types.FULL_RUN
	var reports []maven.CacheReport

	fmt.Println("Parsing Maven cache savings...")
	// Find all XML files in the maven-incremental directory
	pattern := filepath.Join(workspace, "target", "maven-incremental", "*.xml")
	files, err := filepath.Glob(pattern)
	if err != nil {
		return cacheState, nil, fmt.Errorf("failed to find cache reports: %w", err)
	}

	if len(files) == 0 {
		return cacheState, nil, fmt.Errorf("no cache reports found")
	}

	for _, file := range files {
		report, err := parseXMLReport(file)
		if err != nil {
			log.WithError(err).WithField("file", file).Errorln("failed to parse cache report")
			continue
		}

		reports = append(reports, *report)

		// Check if any project in this report is cached
		for _, project := range report.Projects {
			if project.ChecksumMatched && project.LifecycleMatched && project.Source == "REMOTE" {
				cacheState = types.OPTIMIZED
				break
			}
		}
	}

	fmt.Println("Parsed Maven cache savings. state=", cacheState)
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
