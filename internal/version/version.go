package version

import (
	"fmt"
	"runtime"
	"runtime/debug"
	"strings"
)

var Version = "dev"

func init() {
	if Version == "dev" {
		if v := buildVersion(); v != "" {
			Version = v
		}
	}
}

func buildVersion() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	v := info.Main.Version
	if v == "" || v == "(devel)" {
		return ""
	}
	return strings.TrimPrefix(v, "v")
}

// Line returns "<name> <version> (<goversion> <os>/<arch>)".
func Line(name string) string {
	return fmt.Sprintf("%s %s (%s %s/%s)", name, Version, runtime.Version(), runtime.GOOS, runtime.GOARCH)
}
