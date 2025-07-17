package pids

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
	"syscall"
)

// ReadPIDsFromFile reads process IDs (PIDs) from a file at the specified path.
// The file should contain PIDs as comma-separated values.
// It returns a slice of integers representing the PIDs.
// If the file does not exist or cannot be read, it returns an error.
// If the file is empty, it returns an empty slice.
// If any PID is invalid (not a positive integer), it returns an error with details about the invalid PIDs.
func ReadPIDsFromFile(path string) ([]int, error) {
	// Check if file exists
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil, fmt.Errorf("file does not exist: %s", path)
	}

	// Read the entire file
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read file %s: %w", path, err)
	}

	// Convert to string and trim whitespace
	pidString := strings.TrimSpace(string(content))

	// Handle empty file
	if pidString == "" {
		return []int{}, nil
	}

	// Split by comma and handle multiple delimiters
	pidStrings := strings.FieldsFunc(pidString, func(r rune) bool {
		return r == ','
	})

	var pids []int
	var errors []string

	for i, pidStr := range pidStrings {
		// Trim whitespace from each PID string
		pidStr = strings.TrimSpace(pidStr)

		// Skip empty strings
		if pidStr == "" {
			continue
		}

		// Convert to integer
		pid, err := strconv.Atoi(pidStr)
		if err != nil {
			errors = append(errors, fmt.Sprintf("position %d: '%s' - %v", i+1, pidStr, err))
			continue
		}

		// Validate PID (should be positive)
		if pid <= 0 {
			errors = append(errors, fmt.Sprintf("position %d: %d (must be positive)", i+1, pid))
			continue
		}

		pids = append(pids, pid)
	}

	// If there were errors but we got some valid PIDs, return partial results with warning
	if len(errors) > 0 && len(pids) > 0 {
		return pids, fmt.Errorf("parsed %d valid PIDs but encountered errors: %s", len(pids), strings.Join(errors, "; "))
	}

	// If only errors, return them
	if len(errors) > 0 {
		return nil, fmt.Errorf("failed to parse PIDs: %s", strings.Join(errors, "; "))
	}

	return pids, nil
}

// Example usage and helper function to combine with the kill function
func KillProcessesFromFile(ctx context.Context, path string) error {
	pids, err := ReadPIDsFromFile(path)
	if err != nil {
		return fmt.Errorf("failed to read PIDs from file: %w", err)
	}

	if len(pids) == 0 {
		fmt.Println("No valid PIDs found in file")
		return nil
	}

	var errors []string
	for _, pid := range pids {
		err := KillProcess(pid) // Using the function from the previous example
		if err != nil {
			errors = append(errors, fmt.Sprintf("PID %d: %v", pid, err))
		} else {
			fmt.Printf("Successfully killed process %d\n", pid)
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("failed to kill some processes: %s", strings.Join(errors, "; "))
	}

	return nil
}

// AppendPIDToFile appends a process ID to a file at the specified path
// Creates the file if it doesn't exist
// PIDs are stored as comma-separated values
func AppendPIDToFile(pid int, path string) error {
	// Validate PID
	if pid <= 0 {
		return fmt.Errorf("invalid PID: %d (must be positive)", pid)
	}

	// Check if file exists
	fileExists := true
	if _, err := os.Stat(path); os.IsNotExist(err) {
		fileExists = false
	}

	// Open file for appending, create if it doesn't exist
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("failed to open file %s: %w", path, err)
	}
	defer file.Close()

	// Prepare the content to write
	var content string
	if fileExists {
		// If file exists, check if it's empty or needs a comma separator
		fileInfo, err := file.Stat()
		if err != nil {
			return fmt.Errorf("failed to get file info: %w", err)
		}

		if fileInfo.Size() > 0 {
			// File has content, add comma separator
			content = "," + strconv.Itoa(pid)
		} else {
			// File is empty, just add the PID
			content = strconv.Itoa(pid)
		}
	} else {
		// New file, just add the PID
		content = strconv.Itoa(pid)
	}

	// Write to file
	_, err = file.WriteString(content)
	if err != nil {
		return fmt.Errorf("failed to write to file %s: %w", path, err)
	}

	return nil
}

// KillProcess forcefully terminates a process by its PID
// Works on Windows, Linux, and macOS
func KillProcess(pid int) error {
	if pid <= 0 {
		return fmt.Errorf("invalid PID: %d", pid)
	}

	// Find the process
	process, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("failed to find process %d: %w", pid, err)
	}

	// Kill the process based on the operating system
	switch runtime.GOOS {
	case "windows":
		// On Windows, os.Process.Kill() sends a terminate signal
		err = process.Kill()
	case "linux", "darwin":
		// On Unix-like systems, send SIGKILL for forceful termination
		err = process.Signal(syscall.SIGKILL)
	default:
		// Fallback for other Unix-like systems
		err = process.Kill()
	}

	if err != nil {
		return fmt.Errorf("failed to kill process %d: %w", pid, err)
	}

	return nil
}
