package version

import (
	"fmt"
	"runtime"
	"runtime/debug"
	"strings"
)

var Version = "dev"

// fromBuildInfo is true when Version came from the module version Go embeds in the
// binary rather than from the release build's -ldflags: a go install, or a go
// build inside the repo. Such a binary is not the release artifact, so its bytes
// never match the published checksum.
var fromBuildInfo bool

// module is the main module as Go's build info records it. Its Sum, the h1: hash
// of the module source, is set only when the go command downloaded that source,
// as go install pkg@version does; a build inside the repo has none.
var module debug.Module

func init() {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return
	}
	module = info.Main
	if Version == "dev" {
		if v := buildVersion(info.Main); v != "" {
			Version = v
			fromBuildInfo = true
		}
	}
}

func buildVersion(m debug.Module) string {
	v := m.Version
	if v == "" || v == "(devel)" {
		return ""
	}
	return strings.TrimPrefix(v, "v")
}

// Line returns "<name> <version> (<goversion> <os>/<arch>)".
func Line(name string) string {
	return fmt.Sprintf("%s %s (%s %s/%s)", name, Version, runtime.Version(), runtime.GOOS, runtime.GOARCH)
}
