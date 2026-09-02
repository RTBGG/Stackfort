// SPDX-License-Identifier: AGPL-3.0-or-later

package updateapply

import (
	"errors"
	"regexp"
	"strconv"
)

var canonicalVersionPattern = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-beta\.([1-9][0-9]*))?$`)

type semanticVersion struct {
	major, minor, patch int
	beta                int
}

func ParseVersion(value string) (semanticVersion, error) {
	matches := canonicalVersionPattern.FindStringSubmatch(value)
	if matches == nil {
		return semanticVersion{}, errors.New("version must be canonical X.Y.Z or X.Y.Z-beta.N")
	}
	values := make([]int, 4)
	for index := range 3 {
		parsed, err := strconv.Atoi(matches[index+1])
		if err != nil {
			return semanticVersion{}, errors.New("version component exceeds the supported range")
		}
		values[index] = parsed
	}
	if matches[4] != "" {
		parsed, err := strconv.Atoi(matches[4])
		if err != nil {
			return semanticVersion{}, errors.New("beta component exceeds the supported range")
		}
		values[3] = parsed
	}
	return semanticVersion{major: values[0], minor: values[1], patch: values[2], beta: values[3]}, nil
}

func CompareVersions(left, right string) (int, error) {
	leftVersion, err := ParseVersion(left)
	if err != nil {
		return 0, err
	}
	rightVersion, err := ParseVersion(right)
	if err != nil {
		return 0, err
	}
	for _, pair := range [][2]int{
		{leftVersion.major, rightVersion.major},
		{leftVersion.minor, rightVersion.minor},
		{leftVersion.patch, rightVersion.patch},
	} {
		if pair[0] < pair[1] {
			return -1, nil
		}
		if pair[0] > pair[1] {
			return 1, nil
		}
	}
	if leftVersion.beta == rightVersion.beta {
		return 0, nil
	}
	if leftVersion.beta == 0 {
		return 1, nil
	}
	if rightVersion.beta == 0 {
		return -1, nil
	}
	if leftVersion.beta < rightVersion.beta {
		return -1, nil
	}
	return 1, nil
}
