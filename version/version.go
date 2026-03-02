// Copyright 2022 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package version

// Version holds the build version, set via ldflags during build
var Version string

// GetVersion returns the current build version
func GetVersion() string {
	if Version == "" {
		return "dev"
	}
	return Version
}
