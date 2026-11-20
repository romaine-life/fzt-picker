//go:build windows || darwin

package main

import (
	"fmt"
	"io"
	"strings"
)

// Stamped by the release suite with -ldflags -X (.goreleaser.yaml). The linker ignores an
// -X naming a variable that doesn't exist, and drops one nothing reads.
var (
	version     string // the release tag
	buildRepo   string // GITHUB_REPOSITORY, owner/name
	buildServer string // GITHUB_SERVER_URL
	buildTime   string // UTC, RFC 3339
)

func printVersion(out io.Writer) {
	if version == "" {
		fmt.Fprintln(out, "fzt-picker-frontend: development build, not from CI")
		return
	}
	fmt.Fprintln(out, "fzt-picker-frontend", version)
	if buildRepo != "" && buildServer != "" {
		fmt.Fprintln(out, "from", strings.TrimRight(buildServer, "/")+"/"+buildRepo)
	}
	if buildTime != "" {
		fmt.Fprintln(out, "built", buildTime)
	}
}
