// Copyright 2026 Harness Inc. All rights reserved.
// Use of this source code is governed by the PolyForm Free Trial 1.0.0 license
// that can be found in the licenses directory at the root of this repository, also available at
// https://polyformproject.org/wp-content/uploads/2020/05/PolyForm-Free-Trial-1.0.0.txt.

package redirect

import (
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
)

const (
	sidecarPort = 15001
	defaultPort = 3128
	// socketMark is SO_MARK on lite-engine's own dials. The nat rules RETURN it,
	// so control-plane calls are not sent back into the sidecar.
	socketMark = 0x4845

	sidecarUser = "harness-egress"
	chainOut    = "HARNESS_EGRESS"
	chainPre    = "HARNESS_EGRESS_PRE"
	chainIn     = "HARNESS_EGRESS_IN"
	chainFwd    = "HARNESS_EGRESS_FWD"
	chainUDP    = "HARNESS_EGRESS_UDP"

	loopbackCIDR = "127.0.0.0/8"
)

// Cmd is one command. A failure of an Optional command is ignored, which is how
// a chain that already exists or a jump that is not installed yet is handled.
type Cmd struct {
	Args     []string
	Optional bool
}

// Plan is the redirect for one stage. ProxyIPs are the fleet proxy addresses.
// An address on this machine is never redirected, whichever network it belongs
// to and whenever it was added. Exempt are
// the NO_PROXY addresses: traffic that reached its destination directly before
// the redirect still does. ExternalIface is the interface the sidecar port is
// closed on. A redirected container connection reaches the sidecar on a bridge
// interface, so the port must stay open there.
type Plan struct {
	ProxyIPs      []string
	ProxyPort     int
	Exempt        []string
	ExternalIface string
}

func (p *Plan) installCommands() []Cmd {
	var cmds []Cmd
	cmds = append(cmds, Cmd{Args: []string{"sysctl", "-w", "net.ipv6.conf.all.disable_ipv6=1"}})
	cmds = append(cmds, natChain(chainOut, "OUTPUT", p.outputRules())...)
	cmds = append(cmds, natChain(chainPre, "PREROUTING", p.preroutingRules())...)
	if p.ExternalIface != "" {
		cmds = append(cmds, filterChain(chainIn, "INPUT", false, [][]string{
			{"-i", p.ExternalIface, "-p", "tcp", "--dport", strconv.Itoa(sidecarPort), "-j", "DROP"},
		})...)
	}
	udp := [][]string{{"-p", "udp", "--dport", "443", "-j", "DROP"}}
	cmds = append(cmds, filterChain(chainUDP, "OUTPUT", true, udp)...)
	cmds = append(cmds, filterChain(chainFwd, "FORWARD", false, udp)...)
	return cmds
}

func removeCommands() []Cmd {
	var cmds []Cmd
	for _, c := range []struct{ table, chain, hook string }{
		{"nat", chainOut, "OUTPUT"},
		{"nat", chainPre, "PREROUTING"},
	} {
		cmds = append(cmds,
			Cmd{Args: ipt(c.table, "-D", c.hook, "-p", "tcp", "-j", c.chain), Optional: true},
			Cmd{Args: ipt(c.table, "-F", c.chain), Optional: true},
			Cmd{Args: ipt(c.table, "-X", c.chain), Optional: true},
		)
	}
	cmds = append(cmds,
		Cmd{Args: ipt("filter", "-D", "INPUT", "-j", chainIn), Optional: true},
		Cmd{Args: ipt("filter", "-F", chainIn), Optional: true},
		Cmd{Args: ipt("filter", "-X", chainIn), Optional: true},
		Cmd{Args: ipt("filter", "-D", "OUTPUT", "-p", "udp", "--dport", "443", "-j", chainUDP), Optional: true},
		Cmd{Args: ipt("filter", "-F", chainUDP), Optional: true},
		Cmd{Args: ipt("filter", "-X", chainUDP), Optional: true},
		Cmd{Args: ipt("filter", "-D", "FORWARD", "-j", chainFwd), Optional: true},
		Cmd{Args: ipt("filter", "-F", chainFwd), Optional: true},
		Cmd{Args: ipt("filter", "-X", chainFwd), Optional: true},
	)
	return cmds
}

// outputRules exempts the sidecar user and lite-engine's marked sockets, then
// redirects the proxy port and ports 80 and 443.
func (p *Plan) outputRules() [][]string {
	rules := [][]string{
		{"-m", "owner", "--uid-owner", sidecarUser, "-j", "RETURN"},
		{"-m", "mark", "--mark", strconv.Itoa(socketMark), "-j", "RETURN"},
	}
	return append(rules, p.redirectRules()...)
}

// preroutingRules is the container path. An owner match does not apply to
// forwarded packets, so only addresses and ports are matched.
func (p *Plan) preroutingRules() [][]string {
	return p.redirectRules()
}

func (p *Plan) redirectRules() [][]string {
	rules := [][]string{{"-d", loopbackCIDR, "-j", "RETURN"}}
	rules = append(rules, []string{"-m", "addrtype", "--dst-type", "LOCAL", "-j", "RETURN"})
	port := strconv.Itoa(p.ProxyPort)
	to := strconv.Itoa(sidecarPort)
	for _, ip := range p.ProxyIPs {
		rules = append(rules, []string{"-p", "tcp", "-d", ip, "--dport", port, "-j", "REDIRECT", "--to-ports", to})
	}
	for _, dst := range p.Exempt {
		rules = append(rules, []string{"-d", dst, "-j", "RETURN"})
	}
	rules = append(rules, []string{"-p", "tcp", "-m", "multiport", "--dports", "80,443", "-j", "REDIRECT", "--to-ports", to})
	return rules
}

// natChain recreates chain and jumps to it from hook for tcp.
func natChain(chain, hook string, rules [][]string) []Cmd {
	return chainCmds("nat", chain, []string{"-p", "tcp", "-j", chain}, hook, rules)
}

// filterChain recreates chain. udpHook is true when the jump itself is limited
// to udp port 443, matching the single rule in that chain. The jump is inserted
// at the top of the hook: Docker accepts forwarded container traffic in rules
// of its own, and a jump appended after them is never reached.
func filterChain(chain, hook string, udpHook bool, rules [][]string) []Cmd {
	jump := []string{"-j", chain}
	if udpHook {
		jump = []string{"-p", "udp", "--dport", "443", "-j", chain}
	}
	return chainCmds("filter", chain, jump, hook, rules)
}

func chainCmds(table, chain string, jump []string, hook string, rules [][]string) []Cmd {
	cmds := []Cmd{
		{Args: ipt(table, "-N", chain), Optional: true},
		{Args: ipt(table, "-F", chain)},
		{Args: ipt(table, append([]string{"-D", hook}, jump...)...), Optional: true},
	}
	for _, rule := range rules {
		cmds = append(cmds, Cmd{Args: ipt(table, append([]string{"-A", chain}, rule...)...)})
	}
	cmds = append(cmds, Cmd{Args: ipt(table, append([]string{"-I", hook}, jump...)...)})
	return cmds
}

func ipt(table string, args ...string) []string {
	return append([]string{"iptables", "-t", table}, args...)
}

// parseProxy returns the host and port of a proxy URL. The URL's userinfo is
// ignored: the credential is carried separately and must not be read back out
// of the URL. The returned error never contains the URL.
func parseProxy(raw string) (host string, port int, err error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", 0, fmt.Errorf("proxy URL is empty")
	}
	if !strings.Contains(raw, "://") {
		raw = "http://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" {
		return "", 0, fmt.Errorf("proxy URL is invalid")
	}
	if u.Port() == "" {
		return u.Hostname(), defaultPort, nil
	}
	port, err = strconv.Atoi(u.Port())
	if err != nil || port <= 0 || port > 65535 {
		return "", 0, fmt.Errorf("proxy port is invalid")
	}
	return u.Hostname(), port, nil
}

// resolveIPv4 returns the IPv4 addresses iptables will match. A literal address
// is used as given.
func resolveIPv4(host string) ([]string, error) {
	if ip := net.ParseIP(host); ip != nil {
		if ip.To4() == nil {
			return nil, fmt.Errorf("proxy address is not IPv4")
		}
		return []string{ip.To4().String()}, nil
	}
	ips, err := net.LookupIP(host)
	if err != nil {
		return nil, fmt.Errorf("proxy host lookup: %w", err)
	}
	var out []string
	for _, ip := range ips {
		if v4 := ip.To4(); v4 != nil {
			out = append(out, v4.String())
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("proxy host has no IPv4 address")
	}
	return out, nil
}

// exemptions turns a NO_PROXY list into addresses iptables can match: IPv4
// addresses, IPv4 CIDRs, and the IPv4 addresses of plain host names. A suffix
// such as ".example.com" or "*.example.com", and "*", cannot be matched on an
// address and is returned in skipped.
func exemptions(noProxy string, resolve func(host string) ([]string, error)) (addrs, skipped []string) {
	seen := map[string]bool{}
	add := func(dst string) {
		if !seen[dst] {
			seen[dst] = true
			addrs = append(addrs, dst)
		}
	}
	for _, entry := range strings.FieldsFunc(noProxy, func(r rune) bool { return r == ',' || r == ' ' }) {
		switch {
		case strings.HasPrefix(entry, ".") || strings.Contains(entry, "*"):
			skipped = append(skipped, entry)
		default:
			if _, network, err := net.ParseCIDR(entry); err == nil {
				if network.IP.To4() != nil {
					add(network.String())
				}
				continue
			}
			host := entry
			if h, _, err := net.SplitHostPort(entry); err == nil {
				host = h
			}
			if ip := net.ParseIP(host); ip != nil {
				if v4 := ip.To4(); v4 != nil {
					add(v4.String())
				}
				continue
			}
			ips, err := resolve(host)
			if err != nil {
				skipped = append(skipped, entry)
				continue
			}
			for _, ip := range ips {
				add(ip)
			}
		}
	}
	return addrs, skipped
}

// parseDefaultRoute returns the interface of the IPv4 default route in the
// format of /proc/net/route, or "" when there is none.
func parseDefaultRoute(table string) string {
	for _, line := range strings.Split(table, "\n") {
		fields := strings.Fields(line)
		const maskField = 7
		if len(fields) > maskField && fields[1] == "00000000" && fields[maskField] == "00000000" {
			return fields[0]
		}
	}
	return ""
}
