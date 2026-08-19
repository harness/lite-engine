// Copyright 2022 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

//go:build windows

package handler

import (
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
)

// attemptNetworkRecovery tries to restore Windows network connectivity
// after GCE suspend/resume by renewing DHCP, flushing DNS, syncing clock,
// and re-adding DNS servers.
func attemptNetworkRecovery() {
	logrus.Infoln("Attempting Windows network recovery after connectivity failure")
	start := time.Now()

	cmds := []struct {
		name string
		args []string
	}{
		{"ipconfig", []string{"/renew"}},
		{"ipconfig", []string{"/flushdns"}},
		{"w32tm", []string{"/resync", "/nowait"}},
		{"netsh", []string{"interface", "ipv4", "add", "dnsserver", "Ethernet", "8.8.8.8", "index=1"}},
		{"netsh", []string{"interface", "ipv4", "add", "dnsserver", "Ethernet", "1.1.1.1", "index=2"}},
	}

	var errors []string
	for _, c := range cmds {
		out, err := exec.Command(c.name, c.args...).CombinedOutput()
		if err != nil {
			msg := fmt.Sprintf("%s %s failed: %v (output: %s)", c.name, strings.Join(c.args, " "), err, strings.TrimSpace(string(out)))
			errors = append(errors, msg)
			logrus.Warnln(msg)
		}
	}

	if len(errors) > 0 {
		logrus.WithField("errors", len(errors)).WithField("elapsed", time.Since(start)).
			Warnln("Network recovery completed with some errors")
	} else {
		logrus.WithField("elapsed", time.Since(start)).
			Infoln("Network recovery completed successfully")
	}
}
