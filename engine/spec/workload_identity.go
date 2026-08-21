// Copyright 2026 Harness Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package spec

// Workload Identity mint socket (VM / HOSTED_VM broker). lite-engine listens on a Unix socket at
// WISocketDir/WISocketName on the host. For container steps lite-engine bind-mounts WISocketDir to the
// SAME path inside the container, so the in-step hcli reaches the socket at one identical path whether
// the step runs on the host (no image) or in a container (image provided) - no port (the VM firewall
// only opens 9079), no mTLS, no DNS. Linux/Mac only; Windows would need a named pipe (follow-up).
const (
	WISocketDir  = "/tmp/harness-wi"
	WISocketName = "wi.sock"
	// WIHandleEnv is the env var carrying the opaque per-step mint handle. Its presence on a step marks
	// that the step has registered workload identities; used to decide whether to bind-mount the socket.
	WIHandleEnv = "HARNESS_WI_HANDLE"
)
