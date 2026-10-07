package golang

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/harness/ti-client/types"
	golangTypes "github.com/harness/ti-client/types/cache/golang"
	"github.com/mattn/go-zglob"
	"github.com/sirupsen/logrus"
)

const reportPathRegex = "**/.harness/go-cache-report.json"

// ParseSavings finds and parses Go cache savings reports emitted by go-cache-proxy.
// envs should be the step environment (preferred). Values fall back to os.Getenv
// so setup-time setHarnessEnvs still works when envs is nil.
func ParseSavings(workspace string, log *logrus.Logger, envs map[string]string) (types.IntelligenceExecutionState, []golangTypes.Report, int, error) {
	cacheState := types.DISABLED
	reports := make([]golangTypes.Report, 0)
	totalDurationMs := 0

	files := findReportFiles(workspace, envs)
	if len(files) == 0 {
		return cacheState, reports, totalDurationMs, fmt.Errorf("no go cache reports found")
	}

	processed := 0
	for _, file := range files {
		report, err := parseReportFile(file)
		if err != nil {
			log.WithError(err).WithField("file", file).Errorln("failed to parse go cache report")
			continue
		}
		reports = append(reports, *report)
		totalDurationMs += int(report.DurationMs)
		if report.Hits > 0 {
			cacheState = types.OPTIMIZED
		} else if report.Gets > 0 || report.Puts > 0 {
			if cacheState != types.OPTIMIZED {
				cacheState = types.FULL_RUN
			}
		}
		if err := os.Remove(file); err != nil && !os.IsNotExist(err) {
			log.WithError(err).Warnf("failed to remove parsed Go cache report %s", file)
		}
		processed++
	}
	if processed == 0 {
		return types.DISABLED, nil, 0, fmt.Errorf("no go cache reports found")
	}
	return cacheState, reports, totalDurationMs, nil
}

func findReportFiles(workspace string, envs map[string]string) []string {
	candidates := make([]string, 0)
	seen := make(map[string]struct{})
	add := func(path string) {
		if path == "" {
			return
		}
		if _, ok := seen[path]; ok {
			return
		}
		if _, err := os.Stat(path); err == nil {
			seen[path] = struct{}{}
			candidates = append(candidates, path)
		}
	}

	if explicit := envValue(envs, "HARNESS_GO_CACHE_REPORT_PATH"); explicit != "" {
		add(explicit)
	}
	if tmpPath := envValue(envs, "HARNESS_TMP_PATH"); tmpPath != "" {
		isolated := isolateSharedTmpWithEnvs(tmpPath, envs)
		add(filepath.Join(isolated, "go-cache-report.json"))
		if isolated != filepath.Clean(tmpPath) {
			_ = os.Remove(filepath.Join(filepath.Clean(tmpPath), "go-cache-report.json"))
			_ = os.Remove(filepath.Join(filepath.Clean(tmpPath), "harness-go-cache-report.json"))
		}
	}
	if workspace != "" {
		add(filepath.Join(workspace, ".harness", "go-cache-report.json"))
		matches, err := zglob.Glob(filepath.Join(workspace, reportPathRegex))
		if err == nil {
			for _, m := range matches {
				add(m)
			}
		}
	}
	if workdir := envValue(envs, "HARNESS_WORKDIR"); workdir != "" {
		add(filepath.Join(workdir, ".harness", "go-cache-report.json"))
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		add(filepath.Join(home, ".harness", "go-cache-report.json"))
	}
	return candidates
}

// envValue prefers step envs (Cloud VM parse path) then the LE process env
// (populated by setHarnessEnvs at setup).
func envValue(envs map[string]string, key string) string {
	if envs != nil {
		if value := strings.TrimSpace(envs[key]); value != "" {
			return value
		}
	}
	return strings.TrimSpace(os.Getenv(key))
}

func isolateSharedTmp(tmpPath string) string {
	return isolateSharedTmpWithEnvs(tmpPath, nil)
}

func isolateSharedTmpWithEnvs(tmpPath string, envs map[string]string) string {
	clean := filepath.Clean(tmpPath)
	if !isHostSharedTmp(clean) {
		return clean
	}
	return filepath.Join(clean, "harness", executionScopeWithEnvs(envs))
}

func isHostSharedTmp(clean string) bool {
	switch clean {
	case "/tmp", "/var/tmp", `\tmp`, `C:\tmp`, `c:\tmp`:
		return true
	}
	return clean == filepath.Clean(os.TempDir())
}

func executionScope() string {
	return executionScopeWithEnvs(nil)
}

func executionScopeWithEnvs(envs map[string]string) string {
	for _, key := range []string{"HARNESS_EXECUTION_ID", "HARNESS_STAGE_ID", "HARNESS_BUILD_ID"} {
		if value := sanitizePathElement(envValue(envs, key)); value != "" {
			return value
		}
	}
	return "unknown"
}

func sanitizePathElement(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	replacer := strings.NewReplacer("/", "_", `\`, "_", "..", "_")
	return replacer.Replace(value)
}

func parseReportFile(path string) (*golangTypes.Report, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var report golangTypes.Report
	if err := json.Unmarshal(data, &report); err != nil {
		return nil, err
	}
	if report.Version == 0 {
		report.Version = 1
	}
	return &report, nil
}

// GetMetadataFromGoMetrics returns total get ops and hits for telemetry.
func GetMetadataFromGoMetrics(metrics *types.SavingsRequest) (totalGets, hits int) {
	if metrics == nil {
		return 0, 0
	}
	for _, report := range metrics.GoMetrics.Reports {
		totalGets += int(report.Gets)
		hits += int(report.Hits)
	}
	return totalGets, hits
}
