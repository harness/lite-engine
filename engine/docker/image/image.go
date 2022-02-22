// Copyright 2019 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package image

import (
	"net/url"
	"strings"

	"github.com/docker/distribution/reference"
)

// Trim returns the short image name without tag.
func Trim(name string) string {
	ref, err := reference.ParseAnyReference(name)
	if err != nil {
		return name
	}
	named, err := reference.ParseNamed(ref.String())
	if err != nil {
		return name
	}
	named = reference.TrimNamed(named)
	return reference.FamiliarName(named)
}

// Expand returns the fully qualified image name.
func Expand(name string) string {
	ref, err := reference.ParseAnyReference(name)
	if err != nil {
		return name
	}
	named, err := reference.ParseNamed(ref.String())
	if err != nil {
		return name
	}
	named = reference.TagNameOnly(named)
	return named.String()
}

// Match returns true if the image name matches
// an image in the list. Note the image tag is not used
// in the matching logic.
func Match(from string, to ...string) bool {
	from = Trim(from)
	for _, match := range to {
		if from == Trim(match) {
			return true
		}
	}
	return false
}

// MatchTag returns true if the image name matches
// an image in the list, including the tag.
func MatchTag(a, b string) bool {
	return Expand(a) == Expand(b)
}

// MatchHostname returns true if the image hostname
// matches the specified hostname.
func MatchHostname(image, hostname string) bool {
	ref, err := reference.ParseAnyReference(image)
	if err != nil {
		return false
	}
	named, err := reference.ParseNamed(ref.String())
	if err != nil {
		return false
	}
	if hostname == "index.docker.io" {
		hostname = "docker.io"
	}
	// the auth address could be a fully qualified
	// url in which case, we should parse so we can
	// extract the domain name.
	if strings.HasPrefix(hostname, "http://") ||
		strings.HasPrefix(hostname, "https://") {
		parsed, err := url.Parse(hostname)
		if err == nil {
			hostname = parsed.Host
		}
	}
	return reference.Domain(named) == hostname
}

// IsLatest parses the image and returns true if
// the image uses the :latest tag.
func IsLatest(s string) bool {
	return strings.HasSuffix(Expand(s), ":latest")
}
