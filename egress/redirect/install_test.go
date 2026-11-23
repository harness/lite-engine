// Copyright 2026 Harness Inc. All rights reserved.
// Use of this source code is governed by the PolyForm Free Trial 1.0.0 license
// that can be found in the licenses directory at the root of this repository, also available at
// https://polyformproject.org/wp-content/uploads/2020/05/PolyForm-Free-Trial-1.0.0.txt.

package redirect

import (
	"context"
	"errors"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"testing"
)

// enable turns the redirect on for one test, on any host OS, and leaves the
// installed state clean afterwards.
func enable(t *testing.T) {
	t.Helper()
	t.Setenv(envEnabled, "true")
	prev := hostOS
	hostOS = "linux"
	clearActive()
	prevSaved := ipv6Saved
	ipv6Saved = filepath.Join(t.TempDir(), "ipv6")
	t.Cleanup(func() {
		hostOS = prev
		ipv6Saved = prevSaved
		clearActive()
	})
}

// recorder is an installer whose steps are recorded in order.
type recorder struct {
	events  []string
	written string
	inst    installer
}

func newRecorder() *recorder {
	r := &recorder{}
	r.inst = installer{
		run: func(_ context.Context, args ...string) error {
			r.events = append(r.events, "run "+strings.Join(args, " "))
			return nil
		},
		lookup:  func(string) (*user.User, error) { return &user.User{Uid: "10", Gid: "20"}, nil },
		resolve: func(string) ([]string, error) { return []string{"172.16.0.11"}, nil },
		start: func(host string, port, uid, gid int) (int, error) {
			if port != 3128 || uid != 10 || gid != 20 {
				return 0, errors.New("unexpected start arguments")
			}
			r.events = append(r.events, "start "+host)
			return startedPID, nil
		},
		ready: func(_ context.Context, pid int) error {
			if pid != startedPID {
				return errors.New("ready was not given the started pid")
			}
			r.events = append(r.events, "ready")
			return nil
		},
		stop: func() { r.events = append(r.events, "stop") },
		write: func(content string, gid int) error {
			if gid != 20 {
				return errors.New("unexpected gid")
			}
			r.written = content
			r.events = append(r.events, "write")
			return nil
		},
		iface:  func() (string, error) { return "eth0", nil },
		sysctl: func(string) (string, error) { return "0", nil },
	}
	return r
}

func (r *recorder) index(prefix string) int {
	for i, e := range r.events {
		if strings.HasPrefix(e, prefix) {
			return i
		}
	}
	return -1
}

const (
	bareProxy  = "http://172.16.0.11:3128"
	startedPID = 4242
)

var testProxy = Proxy{URL: bareProxy, Username: "acct", Password: "secret"}

func TestInstallDoesNotPassTheCredentialToCommands(t *testing.T) {
	enable(t)
	r := newRecorder()
	if err := r.inst.install(context.Background(), testProxy); err != nil {
		t.Fatal(err)
	}
	if r.written != "acct:secret" {
		t.Fatalf("written = %q", r.written)
	}
	for _, e := range r.events {
		if strings.Contains(e, "secret") || strings.Contains(e, "acct") {
			t.Fatalf("an event contains the credential: %s", e)
		}
	}
	if !Active() {
		t.Fatal("redirect is not active after a successful install")
	}
}

// With the redirect in place and nothing listening, every connection on ports
// 80 and 443 is refused, so the sidecar has to be up first.
func TestInstallStartsAndWaitsForTheSidecarBeforeTheRules(t *testing.T) {
	enable(t)
	r := newRecorder()
	if err := r.inst.install(context.Background(), testProxy); err != nil {
		t.Fatal(err)
	}
	order := []int{r.index("stop"), r.index("write"), r.index("start"), r.index("ready"), r.index("run ")}
	for i := 1; i < len(order); i++ {
		if order[i-1] < 0 || order[i] < order[i-1] {
			t.Fatalf("steps out of order: %v", r.events)
		}
	}
}

func TestInstallDoesNotInstallRulesWhenTheSidecarIsNotReady(t *testing.T) {
	enable(t)
	r := newRecorder()
	r.inst.ready = func(context.Context, int) error { return errors.New("not listening") }
	if err := r.inst.install(context.Background(), testProxy); err == nil {
		t.Fatal("install succeeded")
	}
	for _, e := range r.events {
		if strings.Contains(e, " -A ") || strings.Contains(e, " -I ") {
			t.Fatalf("a rule was installed: %s", e)
		}
	}
	if r.index("run iptables -w 5 -t nat -X HARNESS_EGRESS") < 0 {
		t.Fatalf("a stale redirect was not removed: %v", r.events)
	}
	if Active() {
		t.Fatal("redirect is active after a failed install")
	}
}

func TestInstallRemovesAPartialRedirectOnFailure(t *testing.T) {
	enable(t)
	r := newRecorder()
	failed := false
	r.inst.run = func(_ context.Context, args ...string) error {
		cmd := strings.Join(args, " ")
		r.events = append(r.events, "run "+cmd)
		if !failed && strings.Contains(cmd, "-A HARNESS_EGRESS ") {
			failed = true
			return errors.New("iptables failed")
		}
		return nil
	}
	if err := r.inst.install(context.Background(), testProxy); err == nil {
		t.Fatal("install succeeded")
	}
	// Only the removal deletes a chain.
	if r.index("run iptables -w 5 -t nat -X HARNESS_EGRESS") < 0 || Active() {
		t.Fatalf("partial redirect was not removed: %v", r.events)
	}
}

// A failed credential write or sidecar start after the old sidecar was stopped
// must not leave the earlier stage's redirect pointing at nothing, nor the
// installed state set.
func TestInstallFailureClearsAnEarlierInstall(t *testing.T) {
	enable(t)
	setActive("172.16.0.11", 3128)
	r := newRecorder()
	r.inst.write = func(string, int) error { return errors.New("disk full") }
	if err := r.inst.install(context.Background(), testProxy); err == nil {
		t.Fatal("install succeeded")
	}
	if Active() || r.index("run iptables -w 5 -t nat -X HARNESS_EGRESS") < 0 {
		t.Fatalf("earlier install was left behind: %v", r.events)
	}
}

func TestRemoveClearsTheActiveState(t *testing.T) {
	enable(t)
	r := newRecorder()
	if err := r.inst.install(context.Background(), testProxy); err != nil {
		t.Fatal(err)
	}
	if err := r.inst.remove(context.Background()); err != nil {
		t.Fatal(err)
	}
	if Active() {
		t.Fatal("redirect is active after Remove")
	}
	// The sidecar is stopped once the rules are gone, and IPv6 is restored after
	// that, so nothing is left redirecting when IPv6 comes back.
	if r.events[len(r.events)-2] != "stop" || r.events[len(r.events)-1] != "run sysctl -w "+ipv6Sysctl+"=0" {
		t.Fatalf("remove order: %v", r.events[len(r.events)-2:])
	}
}

func TestInstallIsANoOpWithoutAFleetProxy(t *testing.T) {
	enable(t)
	if err := Install(context.Background(), Proxy{}); err != nil || Active() {
		t.Fatalf("err=%v active=%v", err, Active())
	}
}

func TestInstallKeepsGoingWithoutADefaultRoute(t *testing.T) {
	enable(t)
	r := newRecorder()
	r.inst.iface = func() (string, error) { return "", errors.New("no default route") }
	if err := r.inst.install(context.Background(), testProxy); err != nil {
		t.Fatal(err)
	}
	for _, e := range r.events {
		if strings.Contains(e, "-A HARNESS_EGRESS_IN") || strings.Contains(e, "-I INPUT") {
			t.Fatalf("an INPUT rule was installed without an interface: %s", e)
		}
	}
}

func TestRunAllIgnoresOptionalFailures(t *testing.T) {
	err := runAll(context.Background(), []Cmd{{Args: []string{"iptables"}, Optional: true}},
		func(context.Context, ...string) error { return errors.New("missing") })
	if err != nil {
		t.Fatal(err)
	}
}

// The messages are what iptables prints on a real kernel (iptables 1.8.7): a
// jump to a missing chain, a missing chain, and a rule that is not there.
var goneErrors = []string{
	"iptables v1.8.7 (nf_tables): Chain 'HARNESS_EGRESS' does not exist",
	"iptables: No chain/target/match by that name.",
	"iptables: Bad rule (does a matching rule exist in that chain?).",
}

func TestRunRemoveAcceptsAnAlreadyGoneRule(t *testing.T) {
	for _, msg := range goneErrors {
		err := runRemove(context.Background(), []Cmd{{Args: []string{"iptables", "-X", "HARNESS_EGRESS"}}},
			func(context.Context, ...string) error { return errors.New(msg) })
		if err != nil {
			t.Fatalf("%q: runRemove returned %v", msg, err)
		}
	}
}

// A chain that is still referenced cannot be deleted. Ignoring that would
// report a clean teardown while the redirect is still there.
func TestRunRemoveReportsARealFailure(t *testing.T) {
	busy := "iptables v1.8.7 (nf_tables):  CHAIN_USER_DEL failed (Device or resource busy): chain HARNESS_EGRESS"
	err := runRemove(context.Background(), []Cmd{
		{Args: []string{"iptables", "-X", "HARNESS_EGRESS"}, Optional: true},
	}, func(context.Context, ...string) error { return errors.New(busy) })
	if err == nil {
		t.Fatal("runRemove ignored a chain that could not be deleted")
	}
}

// failTeardown makes every teardown command fail with msg. Installing still
// succeeds, so the failure is only in Remove.
func failTeardown(r *recorder, msg string) {
	r.inst.run = func(_ context.Context, args ...string) error {
		cmd := strings.Join(args, " ")
		r.events = append(r.events, "run "+cmd)
		if args[0] == "iptables" && (strings.Contains(cmd, " -D ") || strings.Contains(cmd, " -F ") || strings.Contains(cmd, " -X ")) {
			return errors.New(msg)
		}
		return nil
	}
}

// A failed teardown leaves the redirect in place, so IPv6 must stay off and
// Remove must say so.
func TestRemoveKeepsIPv6OffWhenTeardownFails(t *testing.T) {
	enable(t)
	r := newRecorder()
	if err := r.inst.install(context.Background(), testProxy); err != nil {
		t.Fatal(err)
	}
	failTeardown(r, "iptables v1.8.7 (nf_tables):  CHAIN_USER_DEL failed (Device or resource busy): chain HARNESS_EGRESS")
	r.events = nil
	if err := r.inst.remove(context.Background()); err == nil {
		t.Fatal("Remove reported success with the teardown failing")
	}
	if r.index("run sysctl -w "+ipv6Sysctl+"=0") >= 0 {
		t.Fatalf("IPv6 was restored while the redirect was still in place: %v", r.events)
	}
}

// Rules that are already gone are a finished teardown, so IPv6 comes back.
func TestRemoveTreatsGoneRulesAsDone(t *testing.T) {
	for _, msg := range goneErrors {
		enable(t)
		r := newRecorder()
		if err := r.inst.install(context.Background(), testProxy); err != nil {
			t.Fatal(err)
		}
		failTeardown(r, msg)
		r.events = nil
		if err := r.inst.remove(context.Background()); err != nil {
			t.Fatalf("%q: Remove returned %v", msg, err)
		}
		if r.index("run sysctl -w "+ipv6Sysctl+"=0") < 0 {
			t.Fatalf("%q: IPv6 was not restored: %v", msg, r.events)
		}
	}
}

func TestScrubKeepsTheProxyAndDropsTheCredential(t *testing.T) {
	enable(t)
	fleet := map[string]string{
		"HTTPS_PROXY": "http://acct:secret@172.16.0.11:3128",
		"http_proxy":  "172.16.0.11:3128",
		"NO_PROXY":    "localhost",
		"FOO":         "bar",
	}
	customer := map[string]string{"HTTPS_PROXY": "http://proxy.corp:8080", "FOO": "bar"}

	// Nothing is installed yet, so a step keeps what it was given.
	Scrub(fleet)
	if fleet["HTTPS_PROXY"] != "http://acct:secret@172.16.0.11:3128" {
		t.Fatalf("Scrub changed the env before install: %v", fleet)
	}

	setActive("172.16.0.11", 3128)
	Scrub(fleet)
	Scrub(customer)
	if fleet["HTTPS_PROXY"] != bareProxy || fleet["http_proxy"] != bareProxy {
		t.Fatalf("fleet proxy was not rewritten to the bare address: %v", fleet)
	}
	if fleet["FOO"] != "bar" || fleet["NO_PROXY"] != "localhost" {
		t.Fatalf("unrelated variables changed: %v", fleet)
	}
	for _, v := range fleet {
		if strings.Contains(v, "secret") || strings.Contains(v, "acct") {
			t.Fatalf("the credential is still in the env: %v", fleet)
		}
	}
	if customer["HTTPS_PROXY"] != "http://proxy.corp:8080" {
		t.Fatalf("a customer proxy was changed: %v", customer)
	}
}

func TestScrubForOnlyWhenTheStageUsesTheRedirect(t *testing.T) {
	const url = "http://acct:secret@172.16.0.11:3128"
	env := func() map[string]string { return map[string]string{"HTTPS_PROXY": url} }

	off := env()
	ScrubFor(off, url, false, true)
	if off["HTTPS_PROXY"] != url {
		t.Fatal("ScrubFor changed the env while the redirect is off")
	}

	enable(t)
	on := env()
	ScrubFor(on, url, false, true)
	if on["HTTPS_PROXY"] != bareProxy {
		t.Fatalf("ScrubFor kept the credential: %v", on)
	}
	private := env()
	ScrubFor(private, url, true, true)
	if private["HTTPS_PROXY"] != url {
		t.Fatal("ScrubFor changed a private-connectivity stage")
	}
	unasked := env()
	ScrubFor(unasked, url, false, false)
	if unasked["HTTPS_PROXY"] != url {
		t.Fatal("ScrubFor changed a stage that did not ask for the sidecar")
	}
}

func TestScrubProcessProxyKeepsTheAddress(t *testing.T) {
	t.Setenv("HTTPS_PROXY", "http://acct:secret@172.16.0.11:3128")
	t.Setenv("http_proxy", "http://proxy.corp:8080")
	scrubProcessProxy("172.16.0.11", 3128)
	if got := os.Getenv("HTTPS_PROXY"); got != bareProxy {
		t.Fatalf("HTTPS_PROXY = %q", got)
	}
	if got := os.Getenv("http_proxy"); got != "http://proxy.corp:8080" {
		t.Fatalf("a customer proxy was changed: %q", got)
	}
}

func TestIPv6IsRestoredToItsEarlierValue(t *testing.T) {
	enable(t)
	r := newRecorder()
	if err := r.inst.install(context.Background(), testProxy); err != nil {
		t.Fatal(err)
	}
	saved, err := os.ReadFile(ipv6Saved)
	if err != nil || string(saved) != "0\n" {
		t.Fatalf("saved ipv6 setting = %q, %v", saved, err)
	}

	// A second stage must keep the original value, not record "disabled".
	r.inst.sysctl = func(string) (string, error) {
		t.Fatal("ipv6 was read again on a later install")
		return "", nil
	}
	if err := r.inst.install(context.Background(), testProxy); err != nil {
		t.Fatal(err)
	}
	if err := r.inst.remove(context.Background()); err != nil {
		t.Fatal(err)
	}
	if r.index("run sysctl -w "+ipv6Sysctl+"=0") < 0 {
		t.Fatalf("ipv6 was not restored: %v", r.events)
	}
	if _, err := os.Stat(ipv6Saved); !os.IsNotExist(err) {
		t.Fatal("the saved ipv6 setting was left behind")
	}
}

func TestRemoveLeavesIPv6AloneWhenItWasNeverChanged(t *testing.T) {
	enable(t)
	r := newRecorder()
	if err := r.inst.remove(context.Background()); err != nil {
		t.Fatal(err)
	}
	if r.index("run sysctl -w "+ipv6Sysctl) >= 0 {
		t.Fatalf("ipv6 was changed: %v", r.events)
	}
}

func TestInstallRejectsAnUnexpectedIPv6Value(t *testing.T) {
	enable(t)
	r := newRecorder()
	r.inst.sysctl = func(string) (string, error) { return "yes", nil }
	if err := r.inst.install(context.Background(), testProxy); err == nil {
		t.Fatal("install accepted an unexpected ipv6 setting")
	}
}

func TestEnabledIsLinuxOnly(t *testing.T) {
	t.Setenv(envEnabled, "true")
	prev := hostOS
	t.Cleanup(func() { hostOS = prev })
	for _, goos := range []string{"windows", "darwin"} {
		hostOS = goos
		if Enabled() {
			t.Fatalf("Enabled on %s", goos)
		}
	}
	hostOS = "linux"
	if !Enabled() {
		t.Fatal("not enabled on linux")
	}
}
