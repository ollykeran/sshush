package version

import (
	"fmt"
	"runtime"
)

var Version = "dev"

// Line returns "<name> <version> (<goversion> <os>/<arch>)".
func Line(name string) string {
	return fmt.Sprintf("%s %s (%s %s/%s)", name, Version, runtime.Version(), runtime.GOOS, runtime.GOARCH)
}
