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
	"time"

	"github.com/sirupsen/logrus"
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

// ipv6Saved holds the value of ipv6Sysctl from before the redirect changed it.
// A test points it at a temporary file.
var ipv6Saved = stateDir + "/ipv6"

const (
	readyTimeout = 5 * time.Second
	readyPoll    = 100 * time.Millisecond
)

// Proxy is the fleet proxy for one stage. The credential is the username and
// password, not a URL. NoProxy is the policy's NO_PROXY list.
type Proxy struct {
	URL      string
	Username string
	Password string
	NoProxy  string
}

type execFunc func(ctx context.Context, args ...string) error

type installer struct {
	run     execFunc
	lookup  func(name string) (*user.User, error)
	resolve func(host string) ([]string, error)
	start   func(host string, port, uid, gid int) error
	ready   func(ctx context.Context) error
	stop    func()
	write   func(content string, gid int) error
	iface   func() (string, error)
	sysctl  func(key string) (string, error)
}

func production() installer {
	return installer{
		run:     runCommand,
		lookup:  user.Lookup,
		resolve: resolveIPv4,
		start:   startSidecar,
		ready:   waitListening,
		stop:    stopSidecar,
		write:   writeCredential,
		iface:   defaultInterface,
		sysctl:  readSysctl,
	}
}

// Install starts the sidecar, waits until it listens, then installs the
// redirect. The redirect goes in last: with it in place and nothing listening,
// every connection on ports 80 and 443 is refused. It does nothing when the
// redirect is off, and it removes what it installed if it fails.
func Install(ctx context.Context, proxy Proxy) error {
	if !Applies(proxy.URL, false) {
		return nil
	}
	return production().install(ctx, proxy)
}

// Remove stops the sidecar and deletes the redirect. A machine where it was
// never installed is not an error.
func Remove(ctx context.Context) error {
	if !Enabled() {
		return nil
	}
	return production().remove(ctx)
}

func (i installer) remove(ctx context.Context) error {
	clearActive()
	err := runAll(ctx, removeCommands(), i.run)
	i.stop()
	_ = os.Remove(authFile)
	if err != nil {
		// The redirect is still in place, so IPv6 stays off. The saved value
		// remains for the next Remove.
		return err
	}
	return i.restoreIPv6(ctx)
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
	plan, err := i.plan(host, port, proxy.NoProxy)
	if err != nil {
		return err
	}
	uid, gid, err := i.ensureUser(ctx)
	if err != nil {
		return err
	}
	i.stop()
	if err := i.apply(ctx, &plan, content, host, port, uid, gid); err != nil {
		// Do not leave a half-installed redirect, or the rules of an earlier
		// stage pointing at a sidecar that is gone, behind a failed stage.
		_ = i.remove(context.WithoutCancel(ctx))
		return err
	}
	scrubProcessProxy(host, port)
	setActive(host, port)
	return nil
}

func (i installer) apply(ctx context.Context, plan *Plan, content, host string, port, uid, gid int) error {
	if err := i.write(content, gid); err != nil {
		return err
	}
	if err := i.launch(ctx, host, port, uid, gid); err != nil {
		return err
	}
	// Read the current setting before the rules change it, and only the first
	// time: a later stage must restore what the machine had, not "disabled".
	if err := i.rememberIPv6(); err != nil {
		return err
	}
	return runAll(ctx, plan.installCommands(), i.run)
}

// rememberIPv6 records the current IPv6 setting once. A second stage keeps the
// recorded value, so Remove restores what the machine had before any redirect.
func (i installer) rememberIPv6() error {
	if _, err := os.Stat(ipv6Saved); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	value, err := i.sysctl(ipv6Sysctl)
	if err != nil {
		return err
	}
	value = strings.TrimSpace(value)
	if value != "0" && value != "1" {
		return fmt.Errorf("unexpected value for %s", ipv6Sysctl)
	}
	if err := os.MkdirAll(filepath.Dir(ipv6Saved), dirMode); err != nil {
		return err
	}
	return os.WriteFile(ipv6Saved, []byte(value+"\n"), pidMode)
}

// restoreIPv6 puts back the setting rememberIPv6 recorded. With nothing
// recorded, IPv6 was never changed and is left as it is.
func (i installer) restoreIPv6(ctx context.Context) error {
	raw, err := os.ReadFile(ipv6Saved)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	value := strings.TrimSpace(string(raw))
	if value != "0" && value != "1" {
		return fmt.Errorf("unexpected saved value for %s", ipv6Sysctl)
	}
	if err := i.run(ctx, "sysctl", "-w", ipv6Sysctl+"="+value); err != nil {
		return err
	}
	return os.Remove(ipv6Saved)
}

func readSysctl(key string) (string, error) {
	out, err := exec.Command("sysctl", "-n", key).Output() //nolint:gosec // key is the fixed sysctl name
	if err != nil {
		return "", fmt.Errorf("read %s: %w", key, err)
	}
	return strings.TrimSpace(string(out)), nil
}

// launch starts the sidecar and waits until it accepts connections.
func (i installer) launch(ctx context.Context, host string, port, uid, gid int) error {
	if err := i.start(host, port, uid, gid); err != nil {
		return err
	}
	return i.ready(ctx)
}

func (i installer) plan(host string, port int, noProxy string) (Plan, error) {
	ips, err := i.resolve(host)
	if err != nil {
		return Plan{}, err
	}
	// Without a default route there is no external interface to close the
	// sidecar port on. The sidecar refuses a connection that was not redirected,
	// so the port is not left usable.
	iface, err := i.iface()
	if err != nil {
		logrus.WithError(err).Warnln("egress redirect: no external interface, sidecar port is not closed")
		iface = ""
	}
	exempt, skipped := exemptions(noProxy, i.resolve)
	if len(skipped) > 0 {
		logrus.WithField("entries", strings.Join(skipped, ",")).
			Infoln("egress redirect: NO_PROXY entries that cannot be matched on an address are redirected")
	}
	return Plan{ProxyIPs: ips, ProxyPort: port, Exempt: exempt, ExternalIface: iface}, nil
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

// waitListening returns once the sidecar this Install started holds the
// sidecar port. It fails at once if the sidecar has exited.
func waitListening(ctx context.Context) error {
	deadline := time.Now().Add(readyTimeout)
	for {
		if sidecarListening() {
			return nil
		}
		if !sidecarAlive() {
			return fmt.Errorf("egress sidecar exited; see %s", logPath)
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("egress sidecar is not listening; see %s", logPath)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(readyPoll):
		}
	}
}
