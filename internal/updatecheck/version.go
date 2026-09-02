// SPDX-License-Identifier: AGPL-3.0-or-later

package updatecheck

import (
	"regexp"
	"strconv"
)

var releaseVersionPattern = regexp.MustCompile(
	`^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-beta\.([1-9][0-9]*))?$`,
)

type semanticVersion struct {
	major, minor, patch uint64
	beta                uint64
	prerelease          bool
}

func parseReleaseVersion(tag string) (semanticVersion, string, bool) {
	matches := releaseVersionPattern.FindStringSubmatch(tag)
	if matches == nil {
		return semanticVersion{}, "", false
	}
	values := make([]uint64, 4)
	for index := 1; index <= 3; index++ {
		value, err := strconv.ParseUint(matches[index], 10, 64)
		if err != nil {
			return semanticVersion{}, "", false
		}
		values[index-1] = value
	}
	parsed := semanticVersion{major: values[0], minor: values[1], patch: values[2]}
	if matches[4] != "" {
		beta, err := strconv.ParseUint(matches[4], 10, 64)
		if err != nil {
			return semanticVersion{}, "", false
		}
		parsed.beta = beta
		parsed.prerelease = true
	}
	return parsed, tag[1:], true
}

func parseInstalledVersion(version string) (semanticVersion, bool) {
	parsed, _, ok := parseReleaseVersion("v" + version)
	return parsed, ok
}

func compareVersions(left, right semanticVersion) int {
	for _, pair := range [][2]uint64{
		{left.major, right.major}, {left.minor, right.minor}, {left.patch, right.patch},
	} {
		if pair[0] < pair[1] {
			return -1
		}
		if pair[0] > pair[1] {
			return 1
		}
	}
	if left.prerelease != right.prerelease {
		if left.prerelease {
			return -1
		}
		return 1
	}
	if !left.prerelease {
		return 0
	}
	if left.beta < right.beta {
		return -1
	}
	if left.beta > right.beta {
		return 1
	}
	return 0
}
