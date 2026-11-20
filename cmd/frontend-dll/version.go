package main

/*
#include <stdlib.h>
*/
import "C"

// Stamped by the release suite with -ldflags -X (.goreleaser.yaml). The linker ignores an
// -X naming a variable that doesn't exist, and drops one nothing reads.
var (
	version     string // the release tag
	buildRepo   string // GITHUB_REPOSITORY, owner/name
	buildServer string // GITHUB_SERVER_URL
	buildTime   string // UTC, RFC 3339
)

// PickerVersion is the DLL's release tag, its repository and build time, one per line, or
// "development build, not from CI"; the caller frees it with FreeString.
//
//export PickerVersion
func PickerVersion() *C.char {
	if version == "" {
		return C.CString("development build, not from CI")
	}
	return C.CString(version + "\n" + buildServer + "/" + buildRepo + "\n" + buildTime)
}
