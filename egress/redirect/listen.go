// Copyright 2026 Harness Inc. All rights reserved.
// Use of this source code is governed by the PolyForm Free Trial 1.0.0 license
// that can be found in the licenses directory at the root of this repository, also available at
// https://polyformproject.org/wp-content/uploads/2020/05/PolyForm-Free-Trial-1.0.0.txt.

package redirect

import (
	"fmt"
	"strings"
)

const (
	// procTCPListen is the socket state LISTEN in /proc/net/tcp and /proc/net/tcp6.
	procTCPListen = "0A"
	// Field positions in a /proc/net/tcp line: local address, state, inode.
	procLocalField = 1
	procStateField = 3
	procInodeField = 9
)

// listenerInodes returns the inodes of the sockets listening on port, from the
// text of /proc/net/tcp or /proc/net/tcp6.
func listenerInodes(table string, port int) []string {
	suffix := fmt.Sprintf(":%04X", port)
	var inodes []string
	for _, line := range strings.Split(table, "\n") {
		fields := strings.Fields(line)
		if len(fields) <= procInodeField || fields[procStateField] != procTCPListen {
			continue
		}
		if strings.HasSuffix(strings.ToUpper(fields[procLocalField]), suffix) {
			inodes = append(inodes, fields[procInodeField])
		}
	}
	return inodes
}
