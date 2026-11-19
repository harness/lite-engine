// Copyright 2026 Harness Inc. All rights reserved.
// Use of this source code is governed by the PolyForm Free Trial 1.0.0 license
// that can be found in the licenses directory at the root of this repository, also available at
// https://polyformproject.org/wp-content/uploads/2020/05/PolyForm-Free-Trial-1.0.0.txt.

// Package redirect installs the hosted Linux egress redirect and starts the sidecar.
//
// It stays off unless CI_EGRESS_SIDECAR is true. While it is on, build traffic to
// ports 80 and 443 is redirected to the sidecar, and proxy environment variables
// are removed so a step does not also use the proxy explicitly. The credential is
// written to a root-owned file and is never placed in an environment variable or a
// log line. Private-connectivity stages call Remove and get no redirect.
package redirect
