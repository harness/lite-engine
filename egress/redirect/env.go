// Copyright 2026 Harness Inc. All rights reserved.
// Use of this source code is governed by the PolyForm Free Trial 1.0.0 license
// that can be found in the licenses directory at the root of this repository, also available at
// https://polyformproject.org/wp-content/uploads/2020/05/PolyForm-Free-Trial-1.0.0.txt.

package redirect

import (
	"os"
	"strings"
)

const envEnabled = "CI_EGRESS_SIDECAR"

// proxyEnvNames are removed while the redirect is on. A step that also had the
// proxy URL would send an explicit proxy request and a redirected connection.
var proxyEnvNames = []string{
	"http_proxy", "https_proxy", "no_proxy",
	"HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY",
}

// Enabled reports whether this process should install the redirect. The runner
// sets CI_EGRESS_SIDECAR. It is unset, and the redirect is off, otherwise.
func Enabled() bool {
	switch strings.TrimSpace(os.Getenv(envEnabled)) {
	case "1", "true", "TRUE", "True":
		return true
	default:
		return false
	}
}

// Strip removes proxy variables from env when the redirect is on. env is the
// map that will be copied into a step or into this process.
func Strip(env map[string]string) {
	if !Enabled() {
		return
	}
	for _, name := range proxyEnvNames {
		delete(env, name)
	}
}

// UnsetProcessProxy removes proxy variables from this process when the redirect
// is on, so lite-engine's own clients dial directly and the socket mark can
// exempt them.
func UnsetProcessProxy() {
	if !Enabled() {
		return
	}
	for _, name := range proxyEnvNames {
		_ = os.Unsetenv(name)
	}
}
