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
	"syscall"

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
		Pdeathsig:  syscall.SIGTERM,
	}
	if err := cmd.Start(); err != nil {
		_ = logFile.Close()
		return err
	}
	_ = logFile.Close()
	return os.WriteFile(pidPath, []byte(strconv.Itoa(cmd.Process.Pid)+"\n"), pidMode) //nolint:gosec // pid file, not a credential
}

func sidecarBinary() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	path := filepath.Join(filepath.Dir(exe), "egress-sidecar")
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("egress-sidecar binary: %w", err)
	}
	if info.IsDir() {
		return "", fmt.Errorf("egress-sidecar binary: %s is a directory", path)
	}
	return path, nil
}
