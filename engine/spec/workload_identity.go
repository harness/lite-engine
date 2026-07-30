// Copyright 2026 Harness Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package spec

// Workload Identity mint socket paths (VM / HOSTED_VM broker). lite-engine listens on a Unix socket at
// WISocketHostDir/WISocketName on the host and bind-mounts WISocketHostDir into each step container at
// WISocketContainerDir. The in-step hcli then reaches the mint endpoint over that socket - no network
// port (the VM firewall only opens 9079), no mTLS, no host.docker.internal. Linux/Mac only; Windows
// containers would need a named pipe (follow-up).
const (
	WISocketHostDir      = "/tmp/harness-wi"
	WISocketName         = "wi.sock"
	WISocketContainerDir = "/run/harness-wi"
)
