// Copyright 2026 Harness Inc. All rights reserved.
// Use of this source code is governed by the PolyForm Free Trial 1.0.0 license
// that can be found in the licenses directory at the root of this repository, also available at
// https://polyformproject.org/wp-content/uploads/2020/05/PolyForm-Free-Trial-1.0.0.txt.

// Package sidecar is the production egress sidecar for hosted Linux build VMs
// (CI-26162). iptables redirects TCP 80, TCP 443, and connections to the fleet
// proxy into this process, which turns them into credentialed requests for the
// fleet proxy.
//
// It is a subcommand of the lite-engine binary. CI-26163 writes the credential,
// installs the redirect, and starts "lite-engine egress-sidecar". With
// CI_EGRESS_SIDECAR unset, nothing starts it.
package sidecar
