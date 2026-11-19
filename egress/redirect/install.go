// Copyright 2026 Harness Inc. All rights reserved.
// Use of this source code is governed by the PolyForm Free Trial 1.0.0 license
// that can be found in the licenses directory at the root of this repository, also available at
// https://polyformproject.org/wp-content/uploads/2020/05/PolyForm-Free-Trial-1.0.0.txt.

package redirect

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	stateDir     = "/run/harness-egress"
	authFile     = "/run/harness-egress/proxy-auth"
	authFileMode = 0o640
	dirMode      = 0o750
	pidMode      = 0o644
	pidPath      = "/run/harness-egress/sidecar.pid"
	logPath      = "/var/log/egress-sidecar.log"
)

// Proxy is the fleet proxy for one stage. The credential is the username and
// password, not a URL.
type Proxy struct {
	URL      string
	Username string
	Password string
}

type execFunc func(ctx context.Context, args ...string) error

type installer struct {
	run     execFunc
	lookup  func(name string) (*user.User, error)
	ownIPs  func() ([]string, error)
	resolve func(host string) ([]string, error)
	start   func(host string, port, uid, gid int) error
	stop    func()
	write   func(content string, gid int) error
}

func production() installer {
	return installer{
		run:     runCommand,
		lookup:  user.Lookup,
		ownIPs:  localIPv4,
		resolve: resolveIPv4,
		start:   startSidecar,
		stop:    stopSidecar,
		write:   writeCredential,
	}
}

// Install writes the credential, installs the redirect, and starts the sidecar.
// It does nothing when the redirect is off.
func Install(ctx context.Context, proxy Proxy) error {
	if !Enabled() {
		return nil
	}
	UnsetProcessProxy()
	return production().install(ctx, proxy)
}

// Remove stops the sidecar and deletes the redirect. A machine where it was
// never installed is not an error.
func Remove(ctx context.Context) error {
	if !Enabled() {
		return nil
	}
	inst := production()
	err := runAll(ctx, removeCommands(), inst.run)
	inst.stop()
	_ = os.Remove(authFile)
	return err
}

func (i installer) install(ctx context.Context, proxy Proxy) error {
	host, port, err := parseProxy(proxy.URL)
	if err != nil {
		return err
	}
	content, err := credentialLine(proxy.Username, proxy.Password)
	if err != nil {
		return err
	}
	ips, err := i.resolve(host)
	if err != nil {
		return err
	}
	own, err := i.ownIPs()
	if err != nil {
		return err
	}
	uid, gid, err := i.ensureUser(ctx)
	if err != nil {
		return err
	}
	i.stop()
	if err := i.write(content, gid); err != nil {
		return err
	}
	plan := Plan{ProxyIPs: ips, ProxyPort: port, OwnIPs: own}
	if err := runAll(ctx, plan.installCommands(), i.run); err != nil {
		return err
	}
	return i.start(host, port, uid, gid)
}

func (i installer) ensureUser(ctx context.Context) (uid, gid int, err error) {
	account, err := i.lookup(sidecarUser)
	if err != nil {
		add := []string{"useradd", "--system", "--no-create-home", "--shell", "/usr/sbin/nologin", "--user-group", sidecarUser}
		if err := i.run(ctx, add...); err != nil {
			return 0, 0, err
		}
		account, err = i.lookup(sidecarUser)
		if err != nil {
			return 0, 0, err
		}
	}
	uid, err = strconv.Atoi(account.Uid)
	if err != nil {
		return 0, 0, fmt.Errorf("sidecar user id: %w", err)
	}
	gid, err = strconv.Atoi(account.Gid)
	if err != nil {
		return 0, 0, fmt.Errorf("sidecar group id: %w", err)
	}
	return uid, gid, nil
}

func credentialLine(username, password string) (string, error) {
	if username == "" || password == "" ||
		strings.ContainsAny(username, ":\r\n") || strings.ContainsAny(password, "\r\n") {
		return "", errors.New("proxy credential missing or incomplete")
	}
	return username + ":" + password, nil
}

func writeCredential(content string, gid int) error {
	if err := os.MkdirAll(stateDir, dirMode); err != nil {
		return err
	}
	if err := os.Chown(stateDir, 0, gid); err != nil {
		return err
	}
	file, err := os.OpenFile(authFile, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, authFileMode)
	if err != nil {
		return err
	}
	_, writeErr := file.WriteString(content + "\n")
	closeErr := file.Close()
	if writeErr != nil {
		return writeErr
	}
	if closeErr != nil {
		return closeErr
	}
	if err := os.Chown(authFile, 0, gid); err != nil {
		return err
	}
	return os.Chmod(authFile, authFileMode)
}

func runAll(ctx context.Context, cmds []Cmd, run execFunc) error {
	for _, cmd := range cmds {
		err := run(ctx, cmd.Args...)
		if err != nil && !cmd.Optional {
			return err
		}
	}
	return nil
}

func runCommand(ctx context.Context, args ...string) error {
	cmd := exec.CommandContext(ctx, args[0], args[1:]...) //nolint:gosec // args are the fixed iptables plan, not user input
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %w (%s)", filepath.Base(args[0]), err, strings.TrimSpace(string(out)))
	}
	return nil
}

func stopSidecar() {
	raw, err := os.ReadFile(pidPath)
	if err != nil {
		return
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil {
		return
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		return
	}
	_ = process.Signal(os.Interrupt)
	_ = os.Remove(pidPath)
}
