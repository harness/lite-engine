// Copyright 2026 Harness Inc. All rights reserved.
// Use of this source code is governed by the PolyForm Free Trial 1.0.0 license
// that can be found in the licenses directory at the root of this repository, also available at
// https://polyformproject.org/wp-content/uploads/2020/05/PolyForm-Free-Trial-1.0.0.txt.

package redirect

import (
	"strings"
	"testing"
)

func TestInstallCommandsRedirectAndExempt(t *testing.T) {
	plan := Plan{ProxyIPs: []string{"172.16.0.11"}, ProxyPort: 3128, OwnIPs: []string{"10.1.0.4"}}
	joined := joinCommands(plan.installCommands())

	for _, want := range []string{
		"owner --uid-owner harness-egress -j RETURN",
		"mark --mark 18501 -j RETURN",
		"-d 127.0.0.0/8 -j RETURN",
		"-d 10.1.0.4 -j RETURN",
		"-d 172.16.0.11 --dport 3128 -j REDIRECT --to-ports 15001",
		"--dports 80,443 -j REDIRECT --to-ports 15001",
		"-A OUTPUT -p tcp -j HARNESS_EGRESS",
		"-A PREROUTING -p tcp -j HARNESS_EGRESS_PRE",
		"--dport 15001 -j DROP",
		"-p udp --dport 443 -j DROP",
		"net.ipv6.conf.all.disable_ipv6=1",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("commands missing %q", want)
		}
	}
	// Containers are not locally generated, so the owner match belongs only on OUTPUT.
	pre := between(joined, "-N HARNESS_EGRESS_PRE", "-N HARNESS_EGRESS_IN")
	if strings.Contains(pre, "uid-owner") {
		t.Fatalf("PREROUTING chain uses an owner match:\n%s", pre)
	}
}

func TestRemoveCommandsAreOptional(t *testing.T) {
	for _, cmd := range removeCommands() {
		if !cmd.Optional {
			t.Fatalf("remove command is required: %v", cmd.Args)
		}
	}
}

func TestParseProxyIgnoresUserinfo(t *testing.T) {
	host, port, err := parseProxy("http://acct:secret@172.16.0.11:3128/ignored")
	if err != nil || host != "172.16.0.11" || port != 3128 {
		t.Fatalf("parseProxy = %s:%d %v", host, port, err)
	}
	host, port, err = parseProxy("proxy.internal")
	if err != nil || host != "proxy.internal" || port != defaultPort {
		t.Fatalf("bare host = %s:%d %v", host, port, err)
	}
	if _, _, err := parseProxy(""); err == nil {
		t.Fatal("empty URL was accepted")
	}
}

func TestCredentialLineRejectsIncomplete(t *testing.T) {
	got, err := credentialLine("acct", "secret")
	if err != nil || got != "acct:secret" {
		t.Fatalf("credentialLine = %q %v", got, err)
	}
	for _, tc := range [][2]string{{"", "x"}, {"user", ""}, {"a:b", "x"}, {"a\nb", "x"}, {"user", "x\ny"}} {
		if _, err := credentialLine(tc[0], tc[1]); err == nil {
			t.Fatalf("credentialLine(%q, %q) succeeded", tc[0], tc[1])
		}
	}
}

func joinCommands(cmds []Cmd) string {
	var b strings.Builder
	for _, cmd := range cmds {
		b.WriteString(strings.Join(cmd.Args, " "))
		b.WriteByte('\n')
	}
	return b.String()
}

func between(s, start, end string) string {
	i := strings.Index(s, start)
	j := strings.Index(s, end)
	if i < 0 || j < 0 || j < i {
		return s
	}
	return s[i:j]
}
