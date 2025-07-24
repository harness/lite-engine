package pids

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
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

// KillProcessesFromFile forcefully terminates all processes with PIDs found in a file.
// It takes a path to the file containing the PIDs and a timeout duration.
// It will return an error if it fails to kill any processes.
// The file can be empty, and the function will return nil in that case.
// The function is goroutine-safe and will wait for all goroutines to finish
// before returning.
func KillProcessesFromFile(ctx context.Context, path string, timeout time.Duration) error {
	pids, err := ReadPIDsFromFile(path)
	if err != nil {
		return fmt.Errorf("failed to read PIDs from file: %w", err)
	}

	if len(pids) == 0 {
		fmt.Println("No valid PIDs found in file")
		return nil
	}

	var wg sync.WaitGroup
	var errors []string
	var mu sync.Mutex // for synchronizing access to errors slice

	for _, pid := range pids {
		wg.Add(1)
		go func(pid int) {
			defer wg.Done()
			err := killProcessWithGracePeriod(pid, timeout)
			if err != nil {
				mu.Lock()
				errors = append(errors, fmt.Sprintf("PID %d: %v", pid, err))
				mu.Unlock()
			}
		}(pid)
	}

	wg.Wait()

	if len(errors) > 0 {
		return fmt.Errorf("failed to kill some processes: %s", strings.Join(errors, "; "))
	}

	return nil
}

// killProcessWithGracePeriod sends a signal to a process to exit cleanly, then waits for the process to exit or a timeout to expire.
// If the process does not exit within the given timeout, it sends a SIGKILL signal to force the process to exit.
// The function returns an error if the process cannot be found or if either signal fails to be sent.
func killProcessWithGracePeriod(pid int, timeout time.Duration) error {
	process, err := os.FindProcess(pid)
	if err != nil {
		return err
	}

	// Send signal to allow the process to exit cleanly
	var signal os.Signal
	if runtime.GOOS == "windows" {
		signal = os.Interrupt
	} else {
		signal = syscall.SIGTERM
	}
	err = process.Signal(signal)
	if err != nil {
		return err
	}

	// Wait for the process to exit or timeout to expire
	done := make(chan error)
	go func() {
		_, err := process.Wait()
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			return err
		}
		return nil
	case <-time.After(timeout):
		// If timeout has expired, send SIGKILL
		err = process.Signal(os.Kill)
		if err != nil {
			return err
		}
		return nil
	}
}
