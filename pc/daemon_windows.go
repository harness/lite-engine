// Copyright 2026 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

//go:build windows

package pc

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
	"golang.org/x/sys/windows"
)

const (
	quad100DNSAddress  = "100.100.100.100"
	nrptRuleComment    = "Harness Cloud Private Connectivity (managed by lite-engine)"
	nrptCommandTimeout = 15 * time.Second
)

var (
	TokenDir   = filepath.Join(os.Getenv("ProgramData"), "Harness", "private-connectivity")
	TokenFile  = filepath.Join(TokenDir, "oidc-token")
	MarkerFile = filepath.Join(TokenDir, "lifecycle")
)

func tailscalePath() (string, error) {
	candidates := []string{
		filepath.Join(os.Getenv("ProgramFiles"), "Tailscale", "tailscale.exe"),
		filepath.Join(os.Getenv("ProgramFiles(x86)"), "Tailscale", "tailscale.exe"),
	}
	for _, candidate := range candidates {
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate, nil
		}
	}
	return exec.LookPath("tailscale.exe")
}

func securePlatformTokenDir() error {
	tokenUser, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil || tokenUser == nil || tokenUser.User.Sid == nil {
		return fmt.Errorf("pc: failed to identify the Windows runtime SID")
	}
	runtimeSID := tokenUser.User.Sid.String()
	args := []string{
		TokenDir,
		"/inheritance:r",
		"/grant:r",
		"*" + runtimeSID + ":(OI)(CI)F",
	}
	if runtimeSID != "S-1-5-18" {
		args = append(args, "*S-1-5-18:(OI)(CI)F")
	}
	out, err := exec.Command("icacls", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("pc: failed to secure state directory ACL: %w (output: %s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func platformRuntimeReady() bool {
	ctx, cancel := context.WithTimeout(context.Background(), runtimeStatusTimeout)
	defer cancel()
	return exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command",
		"Get-Service -Name 'Tailscale' -ErrorAction Stop | Out-Null").Run() == nil
}

func platformRuntimeStart(ctx context.Context) error {
	command := "$service = Get-Service -Name 'Tailscale' -ErrorAction Stop; " +
		"if ($service.Status -ne 'Running') { Start-Service -Name 'Tailscale' -ErrorAction Stop }; " +
		"$service.WaitForStatus('Running', [TimeSpan]::FromSeconds(30)); " +
		"if ($service.Status -ne 'Running') { throw 'Tailscale service did not become running' }"
	commandCtx, cancel := context.WithTimeout(ctx, runtimeServiceTimeout)
	defer cancel()
	if out, err := exec.CommandContext(commandCtx, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", command).CombinedOutput(); err != nil {
		return fmt.Errorf("failed to start Tailscale service: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func platformRuntimeStop(ctx context.Context) error {
	command := "$service = Get-Service -Name 'Tailscale' -ErrorAction Stop; " +
		"if ($service.Status -ne 'Stopped') { Stop-Service -Name 'Tailscale' -Force -ErrorAction Stop }; " +
		"$service.WaitForStatus('Stopped', [TimeSpan]::FromSeconds(30)); " +
		"if ($service.Status -ne 'Stopped') { throw 'Tailscale service did not become stopped' }"
	commandCtx, cancel := context.WithTimeout(ctx, runtimeServiceTimeout)
	defer cancel()
	if out, err := exec.CommandContext(commandCtx, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", command).CombinedOutput(); err != nil {
		return fmt.Errorf("failed to stop Tailscale service: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func platformNetworkPrepare(context.Context, string) error {
	return nil
}

func platformNetworkActivate(ctx context.Context, path string) error {
	// The Tailscale daemon is the source of truth for both customer split DNS and
	// App Connector's invisible DNS routes. On Windows Server images, the daemon's
	// built-in resolver can answer those names while the OS is occasionally missing
	// the corresponding NRPT suffix. Repair only those authoritative suffixes; never
	// install a global '.' rule or redirect public DNS.
	statusCtx, cancelStatus := context.WithTimeout(ctx, nrptCommandTimeout)
	output, err := tailscaleCommandContext(statusCtx, path, "dns", "status", "--json").CombinedOutput()
	cancelStatus()
	if err != nil {
		return fmt.Errorf("failed to read Tailscale DNS status: %w", err)
	}
	var status struct {
		TailscaleDNS   bool                       `json:"TailscaleDNS"`
		SplitDNSRoutes map[string]json.RawMessage `json:"SplitDNSRoutes"`
	}
	if err := json.Unmarshal(output, &status); err != nil {
		return fmt.Errorf("failed to parse Tailscale DNS status: %w", err)
	}
	if !status.TailscaleDNS {
		return fmt.Errorf("Tailscale DNS is not enabled after join")
	}

	namespaces := make([]string, 0, len(status.SplitDNSRoutes))
	seen := make(map[string]struct{}, len(status.SplitDNSRoutes))
	for rawNamespace := range status.SplitDNSRoutes {
		namespace, ok := normalizeNRPTNamespace(rawNamespace)
		if !ok {
			if strings.TrimSpace(rawNamespace) == "." {
				continue
			}
			return fmt.Errorf("Tailscale returned an invalid split-DNS namespace")
		}
		if _, exists := seen[namespace]; exists {
			continue
		}
		seen[namespace] = struct{}{}
		namespaces = append(namespaces, namespace)
	}
	if len(namespaces) == 0 {
		logrus.Infoln("pc: Tailscale reported no Windows split-DNS namespaces requiring NRPT activation")
		return nil
	}
	sort.Strings(namespaces)
	payload, err := json.Marshal(namespaces)
	if err != nil {
		return fmt.Errorf("failed to encode Tailscale DNS namespaces: %w", err)
	}

	// Tailscale intentionally groups several split-DNS suffixes into one NRPT rule.
	// Windows Server accepts that policy but does not consistently apply it to host
	// or container lookups. Install an equivalent single-suffix rule for each domain,
	// but permit overlap only with rule IDs explicitly owned by Tailscale. Rules made
	// here carry our unique comment and are the only rules removed during cleanup.
	script := `$ErrorActionPreference = 'Stop'
$managedComment = '` + nrptRuleComment + `'
$quad100 = '` + quad100DNSAddress + `'
$namespaces = @(([Console]::In.ReadToEnd() | ConvertFrom-Json) | ForEach-Object { [string]$_ })
$tailscaleRuleIds = @((Get-ItemProperty -Path 'HKLM:\SOFTWARE\Tailscale IPN' -Name 'NRPTRuleIDs' -ErrorAction Stop).NRPTRuleIDs |
  ForEach-Object { [string]$_ })
if ($tailscaleRuleIds.Count -eq 0) { throw "Tailscale NRPT ownership metadata is missing" }
$allRules = @(Get-DnsClientNrptRule -ErrorAction Stop)
foreach ($namespace in $namespaces) {
  $matches = @($allRules | Where-Object {
    $normalized = @($_.Namespace | Where-Object { $null -ne $_ } |
      ForEach-Object { $_.Trim().Trim('.').ToLowerInvariant() })
    $normalized -contains $namespace
  })
  $managed = @($matches | Where-Object { $_.Comment -eq $managedComment })
  if ($managed.Count -gt 0) { continue }
  $foreign = @($matches | Where-Object { $tailscaleRuleIds -notcontains [string]$_.Name })
  if ($foreign.Count -gt 0) { throw "conflicting non-Tailscale NRPT rule for requested private DNS namespace" }
  Add-DnsClientNrptRule -Namespace ".$namespace" -NameServers $quad100 -Comment $managedComment -DisplayName 'Harness Private Connectivity' -ErrorAction Stop | Out-Null
}
Clear-DnsClientCache -ErrorAction Stop`
	commandCtx, cancelCommand := context.WithTimeout(ctx, nrptCommandTimeout)
	defer cancelCommand()
	command := exec.CommandContext(
		commandCtx, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script)
	command.Stdin = strings.NewReader(string(payload))
	logrus.WithField("namespace_count", len(namespaces)).Infoln(
		"pc: activating Tailscale-reported Windows split-DNS namespaces")
	if output, err := command.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to activate Windows private DNS routing: %w (%s)",
			err, strings.TrimSpace(string(output)))
	}
	logrus.WithField("namespace_count", len(namespaces)).Infoln(
		"pc: Windows private DNS namespace activation completed")
	return nil
}

func platformNetworkRestore(ctx context.Context) error {
	script := `$ErrorActionPreference = 'Stop'
$managedComment = '` + nrptRuleComment + `'
@(Get-DnsClientNrptRule -ErrorAction Stop | Where-Object { $_.Comment -eq $managedComment }) |
  Remove-DnsClientNrptRule -Force -ErrorAction Stop
Clear-DnsClientCache -ErrorAction Stop`
	commandCtx, cancel := context.WithTimeout(ctx, nrptCommandTimeout)
	defer cancel()
	if output, err := exec.CommandContext(
		commandCtx, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script).CombinedOutput(); err != nil {
		return fmt.Errorf("failed to remove Windows private DNS rules: %w (%s)",
			err, strings.TrimSpace(string(output)))
	}
	return nil
}

func platformNetworkResidue() bool {
	// Lite Engine writes its lifecycle marker before changing Windows networking and
	// retains the cleanup-incomplete marker until NRPT restoration succeeds. Those
	// durable files are the reuse fence. Avoid a cold Get-DnsClientNrptRule call here:
	// on Windows Server it can exceed the setup deadline even on an otherwise clean VM.
	// Exact pre-existing suffix conflicts are still checked before a rule is added.
	return false
}

func normalizeNRPTNamespace(value string) (string, bool) {
	namespace := strings.ToLower(strings.Trim(strings.TrimSpace(value), "."))
	namespace = strings.TrimPrefix(namespace, "*.")
	if namespace == "" || len(namespace) > 253 {
		return "", false
	}
	for _, label := range strings.Split(namespace, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return "", false
		}
		for _, character := range label {
			if (character >= 'a' && character <= 'z') || (character >= '0' && character <= '9') ||
				character == '-' {
				continue
			}
			return "", false
		}
	}
	return namespace, true
}

func legacyEgressResidue(context.Context) bool {
	// Lite Engine has no Windows egress-policy implementation and therefore no
	// historical Windows residue to recover. Reuse remains fenced by the durable
	// PC lifecycle, token, cleanup and used markers checked by localRuntimeClean.
	return false
}

func replaceFileAtomically(source, destination string) error {
	sourcePtr, err := windows.UTF16PtrFromString(source)
	if err != nil {
		return err
	}
	destinationPtr, err := windows.UTF16PtrFromString(destination)
	if err != nil {
		return err
	}
	return windows.MoveFileEx(
		sourcePtr,
		destinationPtr,
		windows.MOVEFILE_REPLACE_EXISTING|windows.MOVEFILE_WRITE_THROUGH,
	)
}
