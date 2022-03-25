// Copyright 2022 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package logstream

// Nudge is an interface which provides a resolution (nudge)
// if a specific term is found.
type Nudge interface {
	// GetSearch returns the search regex to look for
	GetSearch() string

	// GetError provides an error message in case the search term was found
	GetError() error

	// GetResolution returns the resolution in case
	// the search term is encountered
	GetResolution() string
}

func NewNudge(search, resolution string, err error) Nudge {
	return &nudge{
		search:     search,
		resolution: resolution,
		error:      err,
	}
}

type nudge struct {
	search     string
	resolution string
	error      error
}

func (n *nudge) GetSearch() string {
	return n.search
}

func (n *nudge) GetResolution() string {
	return n.resolution
}

func (n *nudge) GetError() error {
	return n.error
}
