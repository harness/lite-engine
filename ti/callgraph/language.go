// Copyright 2022 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package callgraph

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/harness/lite-engine/internal/filesystem"
	tiCfg "github.com/harness/lite-engine/ti/config"
	"github.com/harness/lite-engine/ti/instrumentation"
	"github.com/sirupsen/logrus"
)



// DetectLanguageFromFirstNode analyzes the first call graph node to detect the programming language
// based on file extension from the supported languages in the repository
func DetectLanguageFromFirstNode(cg *Callgraph) string {
	if cg == nil || len(cg.Nodes) == 0 {
		return "unknown"
	}

	// Use the first node's file to detect language
	firstNode := cg.Nodes[0]
	if firstNode.File != "" {
		return detectLanguageFromFile(firstNode.File)
	}

	return "unknown"
}

// detectLanguageFromFile detects programming language from file path/extension
// Only detects languages that are supported by the repository instrumentation
func detectLanguageFromFile(filePath string) string {
	ext := strings.ToLower(filepath.Ext(filePath))
	
	switch ext {
	case ".py":
		return "python"
	case ".java":
		return "java"
	case ".kt", ".kts":
		return "kotlin"
	case ".scala":
		return "scala"
	case ".cs", ".vb", ".fs":
		return "dotnet"
	case ".rb":
		return "ruby"
	default:
		return "unknown"
	}
}

// DetectLanguageFromCallgraphFiles detects language from existing callgraph files
// This function can be called after callgraph upload to detect language for telemetry
func DetectLanguageFromCallgraphFiles(ctx context.Context, stepName string, tiConfig *tiCfg.Cfg, outDir, uniqueStepID string) string {
	if tiConfig.GetIgnoreInstr() {
		return "unknown"
	}

	// Create step-specific data directory path
	stepDataDir := filepath.Join(tiConfig.GetDataDir(), instrumentation.GetUniqueHash(uniqueStepID, tiConfig))
	dataDir := fmt.Sprintf("%s/ti/callgraph/", stepDataDir)

	// Parse callgraph files to detect language  
	fs := filesystem.New()
	// Use a silent logger to avoid nil pointer
	dummyLogger := logrus.New()
	dummyLogger.SetOutput(io.Discard)
	dummyLogger.SetLevel(logrus.ErrorLevel)
	cgFiles, _, err := getCgFiles(dataDir, "json", "csv", dummyLogger)
	if err != nil {
		return "unknown"
	}

	parser := NewCallGraphParser(dummyLogger, fs)
	cg, err := parser.Parse(cgFiles, nil)
	if err != nil {
		return "unknown"
	}

	return DetectLanguageFromFirstNode(cg)
}