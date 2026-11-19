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

func TestInstallDoesNotPassTheCredentialToCommands(t *testing.T) {
	t.Setenv(envEnabled, "true")
	var commands []string
	var written string
	var started string
	inst := installer{
		run: func(_ context.Context, args ...string) error {
			commands = append(commands, strings.Join(args, " "))
			return nil
		},
		lookup:  func(string) (*user.User, error) { return &user.User{Uid: "10", Gid: "20"}, nil },
		ownIPs:  func() ([]string, error) { return []string{"10.1.0.4"}, nil },
		resolve: func(string) ([]string, error) { return []string{"172.16.0.11"}, nil },
		start: func(host string, port, uid, gid int) error {
			started = host
			if port != 3128 || uid != 10 || gid != 20 {
				t.Fatalf("start port=%d uid=%d gid=%d", port, uid, gid)
			}
			return nil
		},
		stop: func() {},
		write: func(content string, gid int) error {
			written = content
			if gid != 20 {
				t.Fatalf("gid = %d", gid)
			}
			return nil
		},
	}
	err := inst.install(context.Background(), Proxy{URL: "http://172.16.0.11:3128", Username: "acct", Password: "secret"})
	if err != nil {
		t.Fatal(err)
	}
	if written != "acct:secret" || started != "172.16.0.11" {
		t.Fatalf("written=%q started=%q", written, started)
	}
	for _, cmd := range commands {
		if strings.Contains(cmd, "secret") || strings.Contains(cmd, "acct") {
			t.Fatalf("command contains the credential: %s", cmd)
		}
	}
}

func TestInstallStopsOnRequiredCommand(t *testing.T) {
	t.Setenv(envEnabled, "true")
	inst := installer{
		run:     func(context.Context, ...string) error { return errors.New("iptables failed") },
		lookup:  func(string) (*user.User, error) { return &user.User{Uid: "10", Gid: "20"}, nil },
		ownIPs:  func() ([]string, error) { return nil, nil },
		resolve: func(string) ([]string, error) { return []string{"172.16.0.11"}, nil },
		start:   func(string, int, int, int) error { t.Fatal("sidecar started after a failed command"); return nil },
		stop:    func() {},
		write:   func(string, int) error { return nil },
	}
	if err := inst.install(context.Background(), Proxy{URL: "http://172.16.0.11:3128", Username: "acct", Password: "secret"}); err == nil {
		t.Fatal("install succeeded")
	}
}

func TestRunAllIgnoresOptionalFailures(t *testing.T) {
	err := runAll(context.Background(), []Cmd{{Args: []string{"iptables"}, Optional: true}},
		func(context.Context, ...string) error { return errors.New("missing") })
	if err != nil {
		t.Fatal(err)
	}
}

func TestStripAndUnsetOnlyWhenEnabled(t *testing.T) {
	env := map[string]string{"HTTPS_PROXY": "http://proxy", "FOO": "bar"}
	Strip(env)
	if env["HTTPS_PROXY"] == "" || env["FOO"] != "bar" {
		t.Fatalf("strip while off changed the map: %v", env)
	}
	t.Setenv(envEnabled, "true")
	Strip(env)
	if _, ok := env["HTTPS_PROXY"]; ok || env["FOO"] != "bar" {
		t.Fatalf("strip while on: %v", env)
	}
}

func TestApplySetsDirectDialOnlyWhenEnabled(t *testing.T) {
	off := &transportProxy{}
	Apply(off.transport())
	if off.proxyCleared() {
		t.Fatal("Apply cleared the proxy while the redirect is off")
	}
	t.Setenv(envEnabled, "true")
	on := &transportProxy{}
	Apply(on.transport())
	if !on.proxyCleared() || !on.dialSet() {
		t.Fatal("Apply did not switch the transport to a direct dial")
	}
}
