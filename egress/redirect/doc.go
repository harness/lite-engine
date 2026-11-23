// Copyright 2026 Harness Inc. All rights reserved.
// Use of this source code is governed by the PolyForm Free Trial 1.0.0 license
// that can be found in the licenses directory at the root of this repository, also available at
// https://polyformproject.org/wp-content/uploads/2020/05/PolyForm-Free-Trial-1.0.0.txt.

// Package redirect installs the hosted Linux egress redirect and starts the sidecar.
//
// It runs only for a stage whose egress policy sets sidecar_enabled, on a VM
// where CI_EGRESS_SIDECAR is true. While it is on, build traffic to
// ports 80 and 443 is redirected to the sidecar. A proxy environment variable
// that points at the fleet proxy loses its credential but keeps the proxy
// address, so ports the redirect does not cover are still proxied, and the
// sidecar adds the credential. The credential is written to a root-owned file and
// is never placed in an environment variable or a log line. The sidecar is this
// binary's egress-sidecar command. It is started, and confirmed to hold its
// port, before the rules go in.
// IPv6 is disabled while the redirect is installed, because the rules are IPv4
// only, and Remove puts the previous setting back.
// Private-connectivity stages call Remove and get no redirect.
package redirect
