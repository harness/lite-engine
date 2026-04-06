package handler

import (
	"context"
	"fmt"
	"net"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/harness/lite-engine/api"
	"github.com/sirupsen/logrus"
)

// applyEgressPolicy merges allowed IPs and CIDRs, then applies OS-level firewall rules.
// This function is best-effort: it logs warnings on failures but never returns errors.
func applyEgressPolicy(ctx context.Context, policy *api.EgressPolicy) {
	if policy == nil || !policy.Enabled {
		logrus.Infoln("egress: egress policy is nil or disabled, skipping")
		return
	}

	logrus.WithField("allowed_ips", len(policy.AllowedIPs)).
		Infoln("egress: starting egress policy application")

	// Deduplicate and filter IPv6 (iptables/netsh IPv4 only for now)
	allAllowed := make([]string, 0, len(policy.AllowedIPs))
	ipv6Skipped := 0
	for _, ip := range deduplicate(policy.AllowedIPs) {
		if isIPv6(ip) {
			ipv6Skipped++
			continue
		}
		allAllowed = append(allAllowed, ip)
	}
	if ipv6Skipped > 0 {
		logrus.WithField("ipv6_skipped", ipv6Skipped).
			Infoln("egress: skipped IPv6 addresses")
	}

	logrus.WithField("total_unique_ips", len(allAllowed)).
		Infoln("egress: deduplicated all allowed IPs")

	if len(allAllowed) == 0 {
		logrus.Warnln("egress: no allowed IPs, skipping firewall rules")
		return
	}

	// Dispatch to OS-specific implementation
	var err error
	start := time.Now()

	switch runtime.GOOS {
	case "linux":
		err = applyIptablesRules(ctx, allAllowed)
	case "windows":
		err = applyNetshRules(ctx, allAllowed)
	default:
		logrus.WithField("os", runtime.GOOS).
			Warnln("egress: unsupported OS for egress policy, skipping")
		return
	}

	if err != nil {
		logrus.WithError(err).Warnln("egress: failed to apply firewall rules, continuing without egress restriction")
		return
	}

	logrus.WithField("allowed_count", len(allAllowed)).
		WithField("os", runtime.GOOS).
		WithField("duration_ms", time.Since(start).Milliseconds()).
		Infoln("egress: successfully applied egress policy")
}

// applyIptablesRules applies iptables OUTPUT chain rules to restrict egress traffic.
// Critical connections (loopback, established, DNS) are always allowed first.
func applyIptablesRules(ctx context.Context, allowedIPs []string) error {
	logrus.Infoln("egress: applying iptables OUTPUT chain rules")

	// Order matters: allow essential/critical traffic first, then whitelist, then drop all
	rules := [][]string{
		// Critical: always allow loopback traffic
		{"-A", "OUTPUT", "-o", "lo", "-j", "ACCEPT"},
		// Critical: always allow established/related connections (return traffic)
		{"-A", "OUTPUT", "-m", "state", "--state", "ESTABLISHED,RELATED", "-j", "ACCEPT"},
		// Critical: always allow DNS queries (needed for domain resolution)
		{"-A", "OUTPUT", "-p", "udp", "--dport", "53", "-j", "ACCEPT"},
		{"-A", "OUTPUT", "-p", "tcp", "--dport", "53", "-j", "ACCEPT"},
	}

	// Add allow rules for each whitelisted IP/CIDR
	for _, ip := range allowedIPs {
		rules = append(rules, []string{"-A", "OUTPUT", "-d", ip, "-j", "ACCEPT"})
	}

	// Drop everything else
	rules = append(rules, []string{"-A", "OUTPUT", "-j", "DROP"})

	logrus.WithField("total_rules", len(rules)).
		Infoln("egress: applying all iptables rules")

	for i, rule := range rules {
		if err := runIptables(ctx, rule...); err != nil {
			logrus.WithField("rule_index", i).WithField("rule", strings.Join(rule, " ")).
				WithError(err).Errorln("egress: failed to apply iptables rule")
			return fmt.Errorf("failed to apply rule %v: %w", rule, err)
		}
	}

	logrus.Infoln("egress: all iptables rules applied successfully")
	return nil
}

// applyNetshRules applies Windows Firewall rules to restrict egress traffic.
// Allow rules are created FIRST, then the default outbound policy is set to block.
func applyNetshRules(ctx context.Context, allowedIPs []string) error {
	logrus.Infoln("egress: applying Windows Firewall (netsh) egress rules")

	// Step 1: Create allow rules BEFORE setting default policy to block.
	// This ensures we don't lock ourselves out.

	// Allow loopback traffic
	if err := runNetsh(ctx, "advfirewall", "firewall", "add", "rule",
		"name=Egress-Allow-Loopback", "dir=out", "action=allow",
		"remoteip=127.0.0.1"); err != nil {
		logrus.WithError(err).Warnln("egress: failed to add loopback rule")
	}

	// Allow DNS (UDP and TCP)
	if err := runNetsh(ctx, "advfirewall", "firewall", "add", "rule",
		"name=Egress-Allow-DNS-UDP", "dir=out", "action=allow",
		"protocol=udp", "remoteport=53"); err != nil {
		logrus.WithError(err).Warnln("egress: failed to add DNS UDP rule")
	}
	if err := runNetsh(ctx, "advfirewall", "firewall", "add", "rule",
		"name=Egress-Allow-DNS-TCP", "dir=out", "action=allow",
		"protocol=tcp", "remoteport=53"); err != nil {
		logrus.WithError(err).Warnln("egress: failed to add DNS TCP rule")
	}

	// Allow each whitelisted IP/CIDR
	for i, ip := range allowedIPs {
		ruleName := fmt.Sprintf("Egress-Allow-%d", i)
		if err := runNetsh(ctx, "advfirewall", "firewall", "add", "rule",
			fmt.Sprintf("name=%s", ruleName), "dir=out", "action=allow",
			fmt.Sprintf("remoteip=%s", ip)); err != nil {
			logrus.WithField("rule", ruleName).WithField("ip", ip).
				WithError(err).Warnln("egress: failed to add allow rule")
		}
	}

	// Step 2: Set default outbound policy to block, keep inbound as-is.
	// This must come AFTER all allow rules are in place.
	if err := runNetsh(ctx, "advfirewall", "set", "allprofiles",
		"firewallpolicy", "allowinbound,blockoutbound"); err != nil {
		return fmt.Errorf("failed to set default outbound policy to block: %w", err)
	}

	logrus.Infoln("egress: all Windows Firewall rules applied successfully")
	return nil
}

// runIptables executes an iptables command with the given arguments.
func runIptables(ctx context.Context, args ...string) error {
	cmd := exec.CommandContext(ctx, "iptables", args...) //nolint:gosec
	output, err := cmd.CombinedOutput()
	if err != nil {
		logrus.WithError(err).WithField("args", strings.Join(args, " ")).
			WithField("output", string(output)).
			Errorln("egress: iptables command failed")
		return fmt.Errorf("iptables %s: %w", strings.Join(args, " "), err)
	}
	logrus.WithField("rule", strings.Join(args, " ")).
		Debugln("egress: iptables rule applied")
	return nil
}

// runNetsh executes a netsh command with the given arguments.
func runNetsh(ctx context.Context, args ...string) error {
	cmd := exec.CommandContext(ctx, "netsh", args...) //nolint:gosec
	output, err := cmd.CombinedOutput()
	if err != nil {
		logrus.WithError(err).WithField("args", strings.Join(args, " ")).
			WithField("output", string(output)).
			Errorln("egress: netsh command failed")
		return fmt.Errorf("netsh %s: %w", strings.Join(args, " "), err)
	}
	logrus.WithField("rule", strings.Join(args, " ")).
		Debugln("egress: netsh rule applied")
	return nil
}

// isIPv6 returns true if the given IP or CIDR is an IPv6 address.
func isIPv6(addr string) bool {
	host := addr
	if i := strings.LastIndex(addr, "/"); i != -1 {
		host = addr[:i]
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.To4() == nil
}

// deduplicate removes duplicate strings from a slice.
func deduplicate(items []string) []string {
	seen := make(map[string]struct{}, len(items))
	result := make([]string, 0, len(items))
	for _, item := range items {
		if _, ok := seen[item]; !ok {
			seen[item] = struct{}{}
			result = append(result, item)
		}
	}
	return result
}
