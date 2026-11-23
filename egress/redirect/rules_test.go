// Copyright 2026 Harness Inc. All rights reserved.
// Use of this source code is governed by the PolyForm Free Trial 1.0.0 license
// that can be found in the licenses directory at the root of this repository, also available at
// https://polyformproject.org/wp-content/uploads/2020/05/PolyForm-Free-Trial-1.0.0.txt.

package redirect

import (
	"errors"
	"strings"
	"testing"
)

func TestInstallCommandsRedirectAndExempt(t *testing.T) {
	plan := &Plan{ProxyIPs: []string{"172.16.0.11"}, ProxyPort: 3128, ExternalIface: "eth0"}
	joined := joinCommands(plan.installCommands())

	for _, want := range []string{
		"owner --uid-owner harness-egress -j RETURN",
		"mark --mark 18501 -j RETURN",
		"-d 127.0.0.0/8 -j RETURN",
		"-m addrtype --dst-type LOCAL -j RETURN",
		"-d 172.16.0.11 --dport 3128 -j REDIRECT --to-ports 15001",
		"--dports 80,443 -j REDIRECT --to-ports 15001",
		"-I OUTPUT -p tcp -j HARNESS_EGRESS",
		"-I PREROUTING -p tcp -j HARNESS_EGRESS_PRE",
		"-i eth0 -p tcp --dport 15001 -j DROP",
		"-I INPUT -j HARNESS_EGRESS_IN",
		"-I FORWARD -j HARNESS_EGRESS_FWD",
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

// A redirected container connection arrives on a bridge interface after the
// REDIRECT. A drop on every interface but lo would refuse all container egress.
func TestSidecarPortIsClosedOnTheExternalInterfaceOnly(t *testing.T) {
	plan := &Plan{ProxyIPs: []string{"172.16.0.11"}, ProxyPort: 3128, ExternalIface: "ens4"}
	in := between(joinCommands(plan.installCommands()), "-N HARNESS_EGRESS_IN", "-N HARNESS_EGRESS_UDP")
	if !strings.Contains(in, "-A HARNESS_EGRESS_IN -i ens4 -p tcp --dport 15001 -j DROP") {
		t.Fatalf("no drop on the external interface:\n%s", in)
	}
	if strings.Contains(in, "! -i") || strings.Contains(in, "-i lo") || strings.Count(in, "-A HARNESS_EGRESS_IN") != 1 {
		t.Fatalf("the drop is not limited to the external interface:\n%s", in)
	}

	bare := Plan{ProxyIPs: []string{"172.16.0.11"}, ProxyPort: 3128}
	for _, cmd := range bare.installCommands() {
		joined := strings.Join(cmd.Args, " ")
		if strings.Contains(joined, "HARNESS_EGRESS_IN") && !cmd.Optional {
			t.Fatalf("an INPUT rule without an interface: %s", joined)
		}
	}
	if !strings.Contains(joinCommands(bare.installCommands()), "-X HARNESS_EGRESS_IN") {
		t.Fatal("an earlier INPUT chain is not removed when there is no interface")
	}
}

func TestExemptAddressesReturnBeforeThePortRedirect(t *testing.T) {
	plan := &Plan{
		ProxyIPs: []string{"172.16.0.11"}, ProxyPort: 3128,
		Exempt: []string{"169.254.169.254", "10.0.0.0/8"},
	}
	out := between(joinCommands(plan.installCommands()), "-N HARNESS_EGRESS\n", "-N HARNESS_EGRESS_PRE")
	iProxy := strings.Index(out, "--dport 3128 -j REDIRECT")
	iExempt := strings.Index(out, "-d 169.254.169.254 -j RETURN")
	iCIDR := strings.Index(out, "-d 10.0.0.0/8 -j RETURN")
	iPorts := strings.Index(out, "--dports 80,443 -j REDIRECT")
	if iProxy < 0 || iExempt < iProxy || iCIDR < iExempt || iPorts < iCIDR {
		t.Fatalf("rule order wrong (proxy %d, exempt %d, cidr %d, ports %d):\n%s", iProxy, iExempt, iCIDR, iPorts, out)
	}
}

func TestExemptionsFromNoProxy(t *testing.T) {
	resolve := func(host string) ([]string, error) {
		if host == "registry.internal" {
			return []string{"10.2.0.5"}, nil
		}
		return nil, errors.New("no such host")
	}
	addrs, skipped := exemptions("localhost, 169.254.169.254,10.0.0.0/8 registry.internal:8443,.corp.example,*.svc,gone.example,10.0.0.0/8,fd00::/8,::1,[fd00::5]:8443", resolve)
	want := []string{"169.254.169.254", "10.0.0.0/8", "10.2.0.5"}
	if strings.Join(addrs, ",") != strings.Join(want, ",") {
		t.Fatalf("addrs = %v, want %v", addrs, want)
	}
	// "localhost" is not resolvable by the stub, and is covered by the loopback rule anyway.
	// IPv6 entries cannot be matched by the IPv4 rules, so they are reported too.
	for _, entry := range []string{".corp.example", "*.svc", "gone.example", "fd00::/8", "::1", "[fd00::5]:8443"} {
		if !contains(skipped, entry) {
			t.Fatalf("%s not reported as skipped: %v", entry, skipped)
		}
	}
}

func TestParseDefaultRoute(t *testing.T) {
	table := "Iface\tDestination\tGateway\tFlags\tRefCnt\tUse\tMetric\tMask\tMTU\tWindow\tIRTT\n" +
		"ens4\t00000000\t0100000A\t0003\t0\t0\t100\t00000000\t0\t0\t0\n" +
		"ens4\t0100000A\t00000000\t0005\t0\t0\t100\tFFFFFFFF\t0\t0\t0\n"
	if got := parseDefaultRoute(table); got != "ens4" {
		t.Fatalf("parseDefaultRoute = %q", got)
	}
	if got := parseDefaultRoute("Iface\tDestination\n"); got != "" {
		t.Fatalf("parseDefaultRoute without a route = %q", got)
	}
}

func contains(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

func TestListenerInodes(t *testing.T) {
	table := "  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode\n" +
		"   0: 00000000:3A99 00000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 11111 1 0 100 0 0 10 0\n" +
		"   1: 0100007F:3A99 0100007F:C0A4 01 00000000:00000000 00:00000000 00000000     0        0 22222 1 0 100 0 0 10 0\n" +
		"   2: 00000000:01BB 00000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 33333 1 0 100 0 0 10 0\n" +
		"   3: 00000000000000000000000000000000:3A99 00000000000000000000000000000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 44444 1 0 100 0 0 10 0\n"
	got := listenerInodes(table, 15001) // 0x3A99
	if strings.Join(got, ",") != "11111,44444" {
		t.Fatalf("listenerInodes = %v, want the two LISTEN sockets on port 15001", got)
	}
}
