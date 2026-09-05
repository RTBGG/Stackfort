// SPDX-License-Identifier: AGPL-3.0-or-later

// stackfort-upgrade-matrix is a build/operator tool, never part of the host API.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/RTBGG/stackfort/internal/upgradematrix"
)

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string, output io.Writer) error {
	flags := flag.NewFlagSet("stackfort-upgrade-matrix", flag.ContinueOnError)
	catalogPath := flags.String("catalog", "packaging/upgrades/supported-releases.json", "support catalog")
	target := flags.String("target", "", "candidate version")
	publishedPath := flags.String("published", "", "complete GitHub release inventory JSON")
	evidencePath := flags.String("evidence", "", "qualification evidence JSON")
	targetDigest := flags.String("target-sha256", "", "candidate archive SHA-256")
	verify := flags.Bool("verify", false, "require exhaustive publication evidence")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("unexpected arguments")
	}
	var catalog upgradematrix.Catalog
	if err := read(*catalogPath, &catalog); err != nil {
		return err
	}
	plan, err := upgradematrix.Build(catalog, *target)
	if err != nil {
		return err
	}
	if *publishedPath != "" {
		// The public API includes additional metadata. Limit input but allow
		// these API-owned fields; policy/evidence JSON uses strict decoding.
		file, err := os.Open(*publishedPath)
		if err != nil {
			return err
		}
		defer file.Close()
		content, err := io.ReadAll(io.LimitReader(file, (16<<20)+1))
		if err != nil {
			return err
		}
		if len(content) > 16<<20 {
			return errors.New("published inventory exceeds 16 MiB")
		}
		var releases []upgradematrix.PublishedRelease
		if err := json.Unmarshal(content, &releases); err != nil {
			return err
		}
		if releases == nil {
			return errors.New("published inventory must be an array")
		}
		if err := upgradematrix.CheckPublished(catalog, *target, releases); err != nil {
			return err
		}
	} else if *verify {
		return errors.New("publication gate requires the complete published release inventory")
	}
	if *verify {
		evidence := upgradematrix.Evidence{SchemaVersion: 1, Kind: "release-candidate", Results: []upgradematrix.Result{}}
		if *evidencePath != "" {
			if err := read(*evidencePath, &evidence); err != nil {
				return err
			}
		}
		if err := upgradematrix.CheckEvidence(plan, evidence, *targetDigest); err != nil {
			return err
		}
	}
	encoder := json.NewEncoder(output)
	encoder.SetIndent("", "  ")
	return encoder.Encode(plan)
}

func read(path string, value any) error {
	// #nosec G304 -- this build/operator CLI reads explicitly selected local catalog/evidence files; it has no privileged service entry point.
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	return upgradematrix.Decode(file, value)
}
