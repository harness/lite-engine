// Copyright 2022 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

// Copyright 2019 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package image

import (
	"net/url"
	"strings"

	"github.com/docker/distribution/reference"
)

var (
	internalImages = []string{"harness/drone-git", "plugins/docker", "plugins/acr", "plugins/ecr", "plugins/gcr",
		"plugins/gar", "plugins/gcs", "plugins/s3", "harness/sto-plugin", "plugins/artifactory", "plugins/cache",
		"harness/ssca-plugin", "harness/slsa-plugin", "harness/ssca-compliance-plugin"}
	garRegistry = "us-docker.pkg.dev/gar-prod-setup/harness-public/"
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

// Overrides registry if image is an internal image
func OverrideRegistry(imageWithTag string) string {
	parts := strings.Split(imageWithTag, ":")
	if len(parts) < 1 || len(parts) > 2 {
		return imageWithTag
	}

	imageName := parts[0]
	tagName := ""
	if len(parts) == 2 {
		tagName = parts[1]
	}

	for _, im := range internalImages {
		if imageName == im {
			if tagName == "" {
				return garRegistry + imageName
			}
			return garRegistry + imageName + ":" + tagName
		}
	}
	return imageWithTag
}
