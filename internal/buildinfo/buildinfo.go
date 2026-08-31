// SPDX-License-Identifier: AGPL-3.0-or-later

// Package buildinfo exposes build provenance injected by the release process.
package buildinfo

// These variables are overridden with -ldflags in release builds.
var (
	Version   = "dev"
	Commit    = "unknown"
	BuildDate = "unknown"
)

// Info is the public, serializable representation of build provenance.
type Info struct {
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	BuildDate string `json:"buildDate"`
}

// Current returns the build provenance for the running binary.
func Current() Info {
	return Info{
		Version:   Version,
		Commit:    Commit,
		BuildDate: BuildDate,
	}
}
