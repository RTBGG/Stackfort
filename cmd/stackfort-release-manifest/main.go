// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/RTBGG/stackfort/internal/releaseartifacts"
)

func main() {
	flags := flag.NewFlagSet("stackfort-release-manifest", flag.ExitOnError)
	packageDirectory := flags.String("package-dir", "", "directory containing native WAF packages and release records")
	vinylPackageDirectory := flags.String("vinyl-package-dir", "", "directory containing native Vinyl packages and release records")
	destination := flags.String("destination", "", "release bundle root")
	version := flags.String("version", "", "Stackfort semantic version")
	architecture := flags.String("architecture", "", "release architecture")
	allowIncomplete := flags.Bool("allow-incomplete", false, "write an explicitly incomplete development manifest")
	flags.Parse(os.Args[1:])
	if flags.NArg() != 0 || *destination == "" || *version == "" || *architecture == "" {
		_, _ = fmt.Fprintln(os.Stderr, "destination, version, and architecture are required")
		os.Exit(2)
	}
	manifest, err := releaseartifacts.Assemble(
		*packageDirectory, *vinylPackageDirectory, *destination, *version, *architecture, *allowIncomplete,
	)
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "assemble release component manifest:", err)
		os.Exit(1)
	}
	_, _ = fmt.Fprintf(os.Stdout, "Wrote release manifest for %s/%s (WAF complete: %t, Vinyl complete: %t)\n",
		manifest.Version, manifest.Architecture, manifest.WAFComplete, manifest.VinylComplete)
}
