// Copyright 2026 Harness Inc. All rights reserved.
// Use of this source code is governed by the PolyForm Free Trial 1.0.0 license
// that can be found in the licenses directory at the root of this repository, also available at
// https://polyformproject.org/wp-content/uploads/2020/05/PolyForm-Free-Trial-1.0.0.txt.

//go:build linux

package redirect

import (
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

func syscallCredential(uid, gid int) (*syscall.Credential, error) {
	if uid < 0 || gid < 0 || uid > math.MaxUint32 || gid > math.MaxUint32 {
		return nil, fmt.Errorf("sidecar user id is out of range")
	}
	return &syscall.Credential{Uid: uint32(uid), Gid: uint32(gid)}, nil
}

// DialControl sets SO_MARK so the nat rules return this socket instead of
// redirecting it. It is a no-op while the redirect is off.
func DialControl(_, _ string, raw syscall.RawConn) error {
	if !Enabled() {
		return nil
	}
	var sockErr error
	if err := raw.Control(func(fd uintptr) {
		sockErr = unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_MARK, socketMark)
	}); err != nil {
		return err
	}
	return sockErr
}

func startSidecar(host string, port, uid, gid int) error {
	binary, err := sidecarBinary()
	if err != nil {
		return err
	}
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, authFileMode)
	if err != nil {
		return err
	}
	cmd := exec.Command(binary) //nolint:gosec // binary is next to this executable, not user input
	cmd.Env = []string{
		"PROXY_AUTH_FILE=" + authFile,
		"UPSTREAM_PROXY_HOST=" + host,
		"UPSTREAM_PROXY_PORT=" + strconv.Itoa(port),
		"PATH=/usr/sbin:/usr/bin:/sbin:/bin",
	}
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cred, err := syscallCredential(uid, gid)
	if err != nil {
		_ = logFile.Close()
		return err
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Credential: cred,
		Setpgid:    true,
	}
	if err := cmd.Start(); err != nil {
		_ = logFile.Close()
		return err
	}
	_ = logFile.Close()
	// Reap the sidecar when it exits, so it does not stay a zombie. It outlives a
	// restart of lite-engine on purpose: the next Install stops it by pid.
	go func() { _ = cmd.Wait() }()
	pid := []byte(strconv.Itoa(cmd.Process.Pid) + "\n")
	if err := os.WriteFile(pidPath, pid, pidMode); err != nil { //nolint:gosec // pid file, not a credential
		// Without the pid file nothing could stop this sidecar later.
		_ = cmd.Process.Kill()
		return err
	}
	return nil
}

func sidecarBinary() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	path := filepath.Join(filepath.Dir(exe), sidecarName)
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("egress-sidecar binary: %w", err)
	}
	if info.IsDir() {
		return "", fmt.Errorf("egress-sidecar binary: %s is a directory", path)
	}
	return path, nil
}

const (
	sidecarName = "egress-sidecar"
	stopWait    = 3 * time.Second
	stopPoll    = 50 * time.Millisecond
)

// stopSidecar stops the sidecar a previous Install started and waits for it to
// go, so the next one can bind the port. The pid is trusted only when the
// process still has the sidecar's name: a pid file can outlive its process.
func stopSidecar() {
	raw, err := os.ReadFile(pidPath)
	if err != nil {
		return
	}
	_ = os.Remove(pidPath)
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil || pid <= 1 {
		return
	}
	comm, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/comm")
	if err != nil || strings.TrimSpace(string(comm)) != sidecarName {
		return
	}
	_ = syscall.Kill(pid, syscall.SIGTERM)
	deadline := time.Now().Add(stopWait)
	for time.Now().Before(deadline) {
		if processGone(pid) {
			return
		}
		time.Sleep(stopPoll)
	}
	_ = syscall.Kill(pid, syscall.SIGKILL)
}

// processGone reports whether pid has exited. A zombie has exited: it holds no
// port and waits only for its parent to reap it.
func processGone(pid int) bool {
	if syscall.Kill(pid, 0) != nil {
		return true
	}
	stat, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return true
	}
	// The state is the first field after the command name, which is in
	// parentheses and may itself contain spaces.
	end := strings.LastIndexByte(string(stat), ')')
	fields := strings.Fields(string(stat)[end+1:])
	return end >= 0 && len(fields) > 0 && fields[0] == "Z"
}

// sidecarPID is the pid of the sidecar in the pid file, or 0 when that process
// is not a running sidecar.
func sidecarPID() int {
	raw, err := os.ReadFile(pidPath)
	if err != nil {
		return 0
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil || pid <= 1 {
		return 0
	}
	comm, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/comm")
	if err != nil || strings.TrimSpace(string(comm)) != sidecarName || processGone(pid) {
		return 0
	}
	return pid
}

// sidecarAlive reports whether the sidecar this Install started is running.
func sidecarAlive() bool { return sidecarPID() != 0 }

// sidecarListening reports whether the sidecar itself holds the listening
// socket on the sidecar port. A listener alone is not enough: a sidecar left by
// an earlier run, or any other program, would answer while the one just started
// failed to bind.
func sidecarListening() bool {
	pid := sidecarPID()
	if pid == 0 {
		return false
	}
	want := map[string]bool{}
	for _, table := range []string{"/proc/net/tcp", "/proc/net/tcp6"} {
		raw, err := os.ReadFile(table)
		if err != nil {
			continue
		}
		for _, inode := range listenerInodes(string(raw), sidecarPort) {
			want["socket:["+inode+"]"] = true
		}
	}
	if len(want) == 0 {
		return false
	}
	fds, err := os.ReadDir("/proc/" + strconv.Itoa(pid) + "/fd")
	if err != nil {
		return false
	}
	for _, fd := range fds {
		if target, err := os.Readlink("/proc/" + strconv.Itoa(pid) + "/fd/" + fd.Name()); err == nil && want[target] {
			return true
		}
	}
	return false
}

// defaultInterface is the interface of the IPv4 default route.
func defaultInterface() (string, error) {
	table, err := os.ReadFile("/proc/net/route")
	if err != nil {
		return "", err
	}
	if iface := parseDefaultRoute(string(table)); iface != "" {
		return iface, nil
	}
	return "", fmt.Errorf("no default route")
}
