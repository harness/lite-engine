package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const MaxSummaryFileBytes = 64 * 1024 // 64KB limit for a single summary file

type AnnotationEntry struct {
	ContextId string `json:"contextId"`
	StepId    string `json:"stepId,omitempty"`
	Timestamp int64  `json:"timestamp"`
	Style     string `json:"style"`
	Summary   string `json:"summary"`
	Priority  int    `json:"priority"`
	Mode      string `json:"mode,omitempty"`
}

// truncateUTF8ByBytes truncates a string to the provided byte limit without breaking UTF-8 runes.
func truncateUTF8ByBytes(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	last := 0
	for i := range s {
		if i > limit {
			break
		}
		last = i
	}
	if last == 0 {
		return ""
	}
	return s[:last]
}

// isAnnotationsEnabled returns true if CI_ENABLE_HARNESS_ANNOTATIONS is set to a truthy value
func isAnnotationsEnabled() bool {
	v := os.Getenv("CI_ENABLE_HARNESS_ANNOTATIONS")
	return v == "true"
}

type AnnotationsEnvelope struct {
	PlanExecutionID string            `json:"planExecutionId,omitempty"`
	Annotations     []AnnotationEntry `json:"annotations"`
}

type CLI struct {
	annotationsFile string
}

func NewCLI() *CLI {
	outputPath := os.Getenv("HARNESS_ANNOTATIONS_FILE")
	if outputPath == "" {
		outputPath = "annotations.json"
	}
	return &CLI{
		annotationsFile: outputPath,
	}
}

func (c *CLI) loadEnvelope() (AnnotationsEnvelope, error) {
	env := AnnotationsEnvelope{}

	if _, err := os.Stat(c.annotationsFile); os.IsNotExist(err) {
		return env, nil
	}

	data, err := os.ReadFile(c.annotationsFile)
	if err != nil {
		return env, err
	}

	if len(data) == 0 {
		return env, nil
	}

	if err := json.Unmarshal(data, &env); err != nil {
		return env, fmt.Errorf("invalid annotations file format: %w", err)
	}
	return env, nil
}

func (c *CLI) saveEnvelope(env AnnotationsEnvelope) error {
	data, err := json.MarshalIndent(env, "", "  ")
	if err != nil {
		return err
	}

	// Ensure parent directory exists
	dir := filepath.Dir(c.annotationsFile)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("failed to create parent dir: %w", err)
		}
	}

	// Atomic write pattern: write to tmp and then rename to final
	tmp := c.annotationsFile + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("failed to write temp file: %w", err)
	}
	if err := os.Rename(tmp, c.annotationsFile); err != nil {
		// On Windows, rename may fail if destination exists. Try removing and renaming again.
		_ = os.Remove(c.annotationsFile)
		if err2 := os.Rename(tmp, c.annotationsFile); err2 != nil {
			_ = os.Remove(tmp)
			return fmt.Errorf("failed to finalize write: %w", err2)
		}
	}
	return nil
}

// minimal harness env for messaging only
func (c *CLI) getStepID() string {
	return os.Getenv("HARNESS_STEP_ID")
}

func (c *CLI) getPlanExecutionID() string {
	return os.Getenv("HARNESS_EXECUTION_ID")
}

func (c *CLI) readSummaryFile(filePath string) (string, error) {
	if filePath == "" {
		return "", nil
	}

	info, err := os.Stat(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to stat summary file '%s': %v", filePath, err)
	}
	if info.Size() > MaxSummaryFileBytes {
		return "", fmt.Errorf("summary file '%s' exceeds %d bytes (64KB) with size %d bytes", filePath, MaxSummaryFileBytes, info.Size())
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to read summary file '%s': %v", filePath, err)
	}

	return string(data), nil
}

func (c *CLI) annotate(contextName, style, summary, mode string, priority int) (map[string]interface{}, error) {
	env, err := c.loadEnvelope()
	if err != nil {
		return nil, err
	}

	// Ensure planExecutionId is present at the root for lite-engine to post annotations
	if strings.TrimSpace(env.PlanExecutionID) == "" {
		if pe := c.getPlanExecutionID(); strings.TrimSpace(pe) != "" {
			env.PlanExecutionID = pe
		}
	}

	// summary is already resolved by the caller. It may be empty.

	// step id is always taken from env (HARNESS_STEP_ID)
	stepIdVal := c.getStepID()

	// normalize context name: trim spaces and cap to 256 runes
	ctx := strings.TrimSpace(contextName)
	if ctx == "" {
		ctx = contextName
	}
	if r := []rune(ctx); len(r) > 256 {
		ctx = string(r[:256])
	}

	// Normalize mode
	switch mode {
	case "replace", "append", "delete":
		// ok
	case "":
		mode = "replace"
	default:
		// unknown -> default to replace
		mode = "replace"
	}

	// epoch millis timestamp
	ts := time.Now().UnixMilli()

	// Find existing entry for this context (match by PMS key)
	idx := -1
	for i := range env.Annotations {
		if env.Annotations[i].ContextId == ctx {
			idx = i
			break
		}
	}

	if idx == -1 {
		// New context entry
		env.Annotations = append(env.Annotations, AnnotationEntry{
			ContextId: ctx,
			Timestamp: ts,
			Style:     style,
			Summary:   summary,
			Priority:  priority,
			Mode:      mode,
			StepId:    stepIdVal,
		})
	} else {
		// Merge into existing entry based on mode
		entry := env.Annotations[idx]
		entry.Timestamp = ts
		if mode == "delete" {
			// mark as delete; content not needed
			entry.Mode = "delete"
			entry.Summary = ""
			entry.Style = ""
			entry.Priority = 0
			if stepIdVal != "" {
				entry.StepId = stepIdVal
			}
		} else if mode == "replace" {
			if style != "" {
				entry.Style = style
			}
			entry.Summary = summary
			entry.Mode = "replace"
			if priority > 0 {
				entry.Priority = priority
			}
			if stepIdVal != "" {
				entry.StepId = stepIdVal
			}
		} else { // append
			if style != "" {
				entry.Style = style
			}
			if summary != "" {
				if entry.Summary != "" {
					entry.Summary += "\n" + summary
				} else {
					entry.Summary = summary
				}
			}
			entry.Mode = "append"
			if priority > 0 {
				entry.Priority = priority
			}
			if stepIdVal != "" {
				entry.StepId = stepIdVal
			}
		}
		env.Annotations[idx] = entry
	}

	if err := c.saveEnvelope(env); err != nil {
		return nil, err
	}

	result := map[string]interface{}{
		"context": ctx,
		"stepid":  stepIdVal,
		"message": fmt.Sprintf("Annotation stored for context '%s' with step ID '%s'", ctx, stepIdVal),
	}
	return result, nil
}

func validateFlags(contextName, style, mode string, priority int) error {
	var issues []string
	if strings.TrimSpace(contextName) == "" {
		issues = append(issues, "--context is required")
	}

	s := strings.ToLower(strings.TrimSpace(style))
	if s == "" {
		s = "info"
	}
	switch s {
	case "info", "success", "warning", "error":
	default:
		issues = append(issues, "--style must be one of: info, success, warning, error")
	}

	m := strings.ToLower(strings.TrimSpace(mode))
	if m == "" {
		m = "replace"
	}
	switch m {
	case "append", "replace", "delete":
	default:
		issues = append(issues, "--mode must be one of: append, replace, delete")
	}

	if priority <= 0 {
		issues = append(issues, "--priority must be > 0")
	}

	if len(issues) > 0 {
		return fmt.Errorf(strings.Join(issues, "; "))
	}
	return nil
}

func main() {
	prog := filepath.Base(os.Args[0])
	if len(os.Args) < 2 {
		fmt.Printf("Usage: %s annotate [flags]\n", prog)
	}

	command := os.Args[1]

	if command != "annotate" {
		fmt.Printf("Usage: %s annotate [flags]\n", prog)
		fmt.Println("Available commands: annotate")
	}

	// Feature flag: gate CLI behavior behind CI_ENABLE_HARNESS_ANNOTATIONS
	if command == "annotate" && !isAnnotationsEnabled() {
		// No-op when disabled; do not fail the step
		fmt.Fprintln(os.Stderr, "[ANN_CLI] annotations disabled by CI_ENABLE_HARNESS_ANNOTATIONS")
		return
	}

	fs := flag.NewFlagSet("annotate", flag.ContinueOnError)
	// suppress default usage output on parse errors; we'll control messaging
	fs.SetOutput(io.Discard)
	context := fs.String("context", "", "Context of the step (used as ID) - required (max 256)")
	style := fs.String("style", "info", "Annotation style (info|success|warning|error)")
	summary := fs.String("summary", "", "Inline summary content (markdown). Use --summary-file to read from a file")
	summaryFile := fs.String("summary-file", "", "Path to summary file (.txt or .md)")
	mode := fs.String("mode", "replace", "Annotation mode (append|replace|delete). Optional; defaults to replace")
	priority := fs.Int("priority", 3, "Annotation priority (int). Optional")

	if err := fs.Parse(os.Args[2:]); err != nil {
		fmt.Fprintf(os.Stderr, "[ANN_CLI] warning: failed to parse flags: %v\n", err)
	}

	if err := validateFlags(*context, *style, *mode, *priority); err != nil {
		fmt.Fprintf(os.Stderr, "[ANN_CLI] error: %v\n", err)
		return
	}

	cli := NewCLI()

	// Resolve summary content: file takes precedence over inline text
	var summaryContent string
	if strings.TrimSpace(*summaryFile) != "" {
		sc, err := cli.readSummaryFile(*summaryFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[ANN_CLI] warning: failed to read --summary-file: %v\n", err)
			return
		}
		// Ensure we do not exceed 64KB for file content (already enforced by file size check, but clamp anyway)
		summaryContent = truncateUTF8ByBytes(sc, MaxSummaryFileBytes)
	} else {
		// Clamp inline summary to 64KB
		summaryContent = truncateUTF8ByBytes(*summary, MaxSummaryFileBytes)
	}

	result, err := cli.annotate(*context, *style, summaryContent, *mode, *priority)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[ANN_CLI] warning: %v\n", err)
	}

	resultJSON, _ := json.MarshalIndent(result, "", "  ")
	fmt.Println(string(resultJSON))
}
