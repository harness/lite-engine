// Copyright 2022 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

//go:build darwin

package exec

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"github.com/sirupsen/logrus"
	"golang.org/x/sys/unix"
)

const (
	// Queries for darwin's sysctl
	GET_ALL_PROCS_QUERY     = "kern.proc.all"
	GET_PROCS_BY_PGID_QUERY = "kern.proc.pgrp"

	// Constants for kqueue operations (from sys/event.h)
	EVFILT_PROC = -5 // Filter types
	EV_ADD      = 0x0001
	EV_ENABLE   = 0x0004
	NOTE_EXIT   = 0x80000000 // Process event flags

	MAX_RECURSION_DEPTH int = 1e6
)

// Kevent structure matching C's struct kevent
type Kevent struct {
	Ident  uintptr // identifier for this event
	Filter int16   // filter for event
	Flags  uint16  // action flags for kqueue
	Fflags uint32  // filter flag value
	Data   int64   // filter data value
	Udata  uintptr // opaque user data identifier
}

func WaitForProcess(ctx context.Context, cmd *exec.Cmd, cmdSignal chan<- cmdResult, shouldWaitOnProcessGroup bool) {
	if cmd.Process == nil {
		logrus.WithContext(ctx).Warnln("wait for process requested but cmd.Process is nil")
		return
	}
	if shouldWaitOnProcessGroup {
		waitOnProcessGroup(ctx, cmd, cmdSignal)
	} else {
		// If `shouldWaitOnProcessGroup` is not enabled, we just call the old `waitForCmd` here.
		waitForCmd(cmd, cmdSignal)
	}
}

// This is a new solution for waiting on subprocesses start for a host-run step locally.
// Based on: https://jmmv.dev/2019/11/wait-for-process-group-darwin.html
// Here is an outline of the steps:
// 1. Start process group.
// 2. Wait for the process group leader to become a zombie with waitForProcessReadyToExit. At this point, the PGID is still assigned to us.
// 3. Wait for all other processes in the group to exit by calling waitProcessGroupReadyToExit.
// 4. Collect the leader’s status by using `waitForCmd` (waitpid(2)).
func waitOnProcessGroup(ctx context.Context, cmd *exec.Cmd, cmdSignal chan<- cmdResult) {
	pgid := cmd.Process.Pid
	// Wait for lead process to be ready to exit (become a zombie)
	if err := waitForProcessReadyToExit(pgid); err != nil {
		logrus.WithContext(ctx).Errorf("failed waiting for leading process of group %d to exit: %v", pgid, err)
		cmdSignal <- cmdResult{state: nil, err: err}
		return
	}
	logrus.WithContext(ctx).Debugf("lead process of group %d is at terminal state. Waiting for other processes in the group to exit", pgid)
	// Wait for all remaining processes from the process group to exit
	if err := waitProcessGroupReadyToExit(ctx, pgid); err != nil {
		logrus.WithContext(ctx).Errorf("failed waiting for remaining processes of group %d to exit: %v", pgid, err)
		cmdSignal <- cmdResult{state: nil, err: err}
		return
	}
	// Collect group leader's exit status
	waitForCmd(cmd, cmdSignal)
	logrus.WithContext(ctx).Errorf("all processes in process group %d exited", pgid)
}

// Waits for a process to terminate but does *not* collect its exit status,
// thus leaving the process as a zombie.
//
// According to the kqueue(2) documentation (and I confirmed it experimentally),
// registering for an event reports any pending such events, so this is not racy
// if the process happened to exit before we got to installing the kevent.
func waitForProcessReadyToExit(pid int) error {
	// Create kqueue
	kq, err := syscall.Kqueue()
	if err != nil {
		return fmt.Errorf("kqueue creation failed: %v", err)
	}
	defer syscall.Close(kq)

	// Set up kevent for process monitoring
	kc := Kevent{
		Ident:  uintptr(pid),
		Filter: EVFILT_PROC,
		Flags:  EV_ADD | EV_ENABLE,
		Fflags: NOTE_EXIT,
		Data:   0,
		Udata:  0,
	}

	// Register the event and wait for it
	ke := Kevent{}
	nev, err := kevent(kq, &kc, 1, &ke, 1, nil)
	if err != nil {
		return fmt.Errorf("kevent failed: %v", err)
	}
	if nev != 1 {
		return fmt.Errorf("expected 1 event, got %d", nev)
	}
	if ke.Ident != uintptr(pid) {
		return fmt.Errorf("event ident mismatch: expected %d, got %d", pid, ke.Ident)
	}
	if ke.Fflags&NOTE_EXIT == 0 {
		return fmt.Errorf("event does not have NOTE_EXIT flag set")
	}
	return nil
}

// waitForProcessGroup waits until only the process group leader (pgid)
// remains in the group. It assumes the leader may remain as a zombie.
func waitProcessGroupReadyToExit(ctx context.Context, pgid int) error {
	iteration := 0
	for {
		procs, err := unix.SysctlKinfoProcSlice(GET_PROCS_BY_PGID_QUERY, pgid)
		if err != nil {
			return fmt.Errorf("failed to list processes in group %d: %w", pgid, err)
		}
		nprocs := len(procs)
		if nprocs == 0 {
			// No processes found at all (group disappeared entirely).
			return fmt.Errorf("unexpected: no processes in group %d but leader expected", pgid)
		}
		if nprocs == 1 {
			// Only leader remains
			leader := procs[0]
			if int(leader.Proc.P_pid) != pgid {
				return fmt.Errorf("unexpected: when waiting for process group, expected leader %d, found pid %d", pgid, leader.Proc.P_pid)
			}
			return nil
		}
		// More than one process — wait a little and retry.
		iteration++
		if iteration%30 == 0 {
			// Every 30 seconds, we print a debug log with PIDs of the remaining processes
			var pids []string
			// Extract PIDs to format as string
			for _, proc := range procs {
				pids = append(pids, fmt.Sprintf("%d", proc.Proc.P_pid))
			}
			logrus.WithContext(ctx).Debugf("%d processes still running in process group %d (PIDs %s)", nprocs, pgid, strings.Join(pids, ","))
		}
		time.Sleep(1 * time.Second)
	}
}

// kevent system call wrapper
func kevent(kq int, changelist *Kevent, nchanges int, eventlist *Kevent, nevents int, timeout *syscall.Timespec) (int, error) {
	var changePtr, eventPtr uintptr

	if changelist != nil {
		changePtr = uintptr(unsafe.Pointer(changelist))
	}

	if eventlist != nil {
		eventPtr = uintptr(unsafe.Pointer(eventlist))
	}

	var timeoutPtr uintptr
	if timeout != nil {
		timeoutPtr = uintptr(unsafe.Pointer(timeout))
	}

	r1, _, errno := syscall.Syscall6(
		syscall.SYS_KEVENT,
		uintptr(kq),
		changePtr,
		uintptr(nchanges),
		eventPtr,
		uintptr(nevents),
		timeoutPtr,
	)

	if errno != 0 {
		return 0, errno
	}

	return int(r1), nil
}

func AbortProcess(ctx context.Context, cmd *exec.Cmd, cmdSignal <-chan cmdResult, retryIntervalSecs, maxSigtermAttempts int, useExplicitProcessTree bool) {
	if cmd.Process == nil {
		logrus.WithContext(ctx).Warnln("abort requested but cmd.Process is nil")
		return
	}
	if useExplicitProcessTree {
		abortProcessTree(ctx, cmd.Process.Pid)
	} else {
		// Traditional abort strategy -> We abort the process group of the step's subprocess
		go abortProcessGroup(ctx, cmd.Process.Pid, retryIntervalSecs, maxSigtermAttempts, cmdSignal)
	}
}

/***** EXPLICIT PROCESS TREE ABORTION STRATEGY FOR MacOS (darwin)  ******/
func abortProcessTree(ctx context.Context, pgid int) {
	logrus.WithContext(ctx).Debugf("aborting process tree starting from PID %d", pgid)
	pids, err := getProcessTree(ctx, int32(pgid))
	if err != nil {
		logrus.WithContext(ctx).Errorf("failed to abort process tree starting from PID %d: %v", pgid, err)
	}

	var pidsStr []string
	// Extract PIDs to format as string
	for _, pid := range pids {
		pidsStr = append(pidsStr, fmt.Sprintf("%d", pid))
	}
	logrus.WithContext(ctx).Debugf("sending SIGKILL to process tree starting from PID %d (PIDs: %s)", pgid, strings.Join(pidsStr, ","))
	var errors []string
	for _, pid := range pids {
		if err := syscall.Kill(int(pid), syscall.SIGKILL); err != nil {
			errors = append(errors, fmt.Sprintf("failed to kill PID %d: %v", pid, err))
		}
	}
	if len(errors) > 0 {
		logrus.WithContext(ctx).Errorf("failed to send SIGKILL to some processes in tree starting from PID %d. Errors: %s", pgid, strings.Join(errors, ","))
	}
	// For extra safety, also send SIGKILL to the parent process group
	syscall.Kill(-pgid, syscall.SIGKILL)
}

// GetChildProcesses returns all child process PIDs for a given parent PID using sysctl
func getProcessTree(ctx context.Context, parentPID int32) ([]int32, error) {
	// Get all processes using sysctl
	allProcs, err := getAllProcesses()
	if err != nil {
		return nil, fmt.Errorf("failed to get all processes: %v", err)
	}
	// Find all children of the given parent PID
	children := []int32{parentPID}
	findChildrenRecursively(ctx, allProcs, parentPID, &children, 0)
	return children, nil
}

// getAllProcesses retrieves all processes using sysctl KERN_PROC
func getAllProcesses() ([]unix.KinfoProc, error) {
	return unix.SysctlKinfoProcSlice(GET_ALL_PROCS_QUERY)
}

// findChildrenRecursively recursively finds all child processes
func findChildrenRecursively(ctx context.Context, allProcs []unix.KinfoProc, parentPID int32, children *[]int32, depth int) {
	if depth > MAX_RECURSION_DEPTH {
		logrus.WithContext(ctx).Warnf(
			"max depth (%d) reached when traversing process tree. Returning incomplete list of children for parentPID %d",
			MAX_RECURSION_DEPTH,
			parentPID,
		)
		return
	}
	for _, proc := range allProcs {
		if proc.Eproc.Ppid == parentPID {
			childPID := proc.Proc.P_pid
			*children = append(*children, childPID)
			// Recursively find children of this child
			findChildrenRecursively(ctx, allProcs, childPID, children, depth+1)
		}
	}
}
