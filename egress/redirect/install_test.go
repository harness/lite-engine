// Copyright 2026 Harness Inc. All rights reserved.
// Use of this source code is governed by the PolyForm Free Trial 1.0.0 license
// that can be found in the licenses directory at the root of this repository, also available at
// https://polyformproject.org/wp-content/uploads/2020/05/PolyForm-Free-Trial-1.0.0.txt.

package redirect

import (
	"context"
	"errors"
	"os/user"
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
	t.Cleanup(func() {
		hostOS = prev
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
		ownIPs:  func() ([]string, error) { return []string{"10.1.0.4"}, nil },
		resolve: func(string) ([]string, error) { return []string{"172.16.0.11"}, nil },
		start: func(host string, port, uid, gid int) error {
			if port != 3128 || uid != 10 || gid != 20 {
				return errors.New("unexpected start arguments")
			}
			r.events = append(r.events, "start "+host)
			return nil
		},
		ready: func(context.Context) error { r.events = append(r.events, "ready"); return nil },
		stop:  func() { r.events = append(r.events, "stop") },
		write: func(content string, gid int) error {
			if gid != 20 {
				return errors.New("unexpected gid")
			}
			r.written = content
			r.events = append(r.events, "write")
			return nil
		},
		iface: func() (string, error) { return "eth0", nil },
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

var testProxy = Proxy{URL: "http://172.16.0.11:3128", Username: "acct", Password: "secret"}

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
	r.inst.ready = func(context.Context) error { return errors.New("not listening") }
	if err := r.inst.install(context.Background(), testProxy); err == nil {
		t.Fatal("install succeeded")
	}
	if r.index("run ") >= 0 {
		t.Fatalf("rules were installed: %v", r.events)
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
	if r.index("run iptables -t nat -X HARNESS_EGRESS") < 0 || Active() {
		t.Fatalf("partial redirect was not removed: %v", r.events)
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
	if r.events[len(r.events)-1] != "stop" {
		t.Fatalf("sidecar was not stopped last: %v", r.events)
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
	if strings.Contains(strings.Join(r.events, "\n"), "HARNESS_EGRESS_IN") {
		t.Fatalf("an INPUT rule was installed without an interface: %v", r.events)
	}
}

func TestRunAllIgnoresOptionalFailures(t *testing.T) {
	err := runAll(context.Background(), []Cmd{{Args: []string{"iptables"}, Optional: true}},
		func(context.Context, ...string) error { return errors.New("missing") })
	if err != nil {
		t.Fatal(err)
	}
}

func TestStripRemovesOnlyTheFleetProxy(t *testing.T) {
	enable(t)
	fleet := map[string]string{
		"HTTPS_PROXY": "http://acct:secret@172.16.0.11:3128",
		"http_proxy":  "172.16.0.11:3128",
		"NO_PROXY":    "localhost",
		"FOO":         "bar",
	}
	customer := map[string]string{"HTTPS_PROXY": "http://proxy.corp:8080", "FOO": "bar"}

	// Nothing is installed yet, so a step keeps its proxy.
	Strip(fleet)
	if len(fleet) != 4 {
		t.Fatalf("Strip changed the env before install: %v", fleet)
	}

	setActive("172.16.0.11", 3128)
	Strip(fleet)
	Strip(customer)
	if _, ok := fleet["HTTPS_PROXY"]; ok || fleet["FOO"] != "bar" || fleet["NO_PROXY"] != "localhost" {
		t.Fatalf("fleet env after Strip: %v", fleet)
	}
	if _, ok := fleet["http_proxy"]; ok {
		t.Fatalf("bare host:port was not stripped: %v", fleet)
	}
	if customer["HTTPS_PROXY"] != "http://proxy.corp:8080" {
		t.Fatalf("a customer proxy was stripped: %v", customer)
	}
}

func TestStripForOnlyWhenTheStageUsesTheRedirect(t *testing.T) {
	const url = "http://172.16.0.11:3128"
	env := func() map[string]string { return map[string]string{"HTTPS_PROXY": url} }

	off := env()
	StripFor(off, url, false)
	if len(off) != 1 {
		t.Fatal("StripFor changed the env while the redirect is off")
	}

	enable(t)
	on := env()
	StripFor(on, url, false)
	if len(on) != 0 {
		t.Fatalf("StripFor kept the fleet proxy: %v", on)
	}
	private := env()
	StripFor(private, url, true)
	if len(private) != 1 {
		t.Fatal("StripFor changed a private-connectivity stage")
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
