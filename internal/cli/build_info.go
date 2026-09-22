package cli

import (
	"io"
	"os"
	"runtime"
	"runtime/debug"
)

// BuildVersion is set by release builds; Go's VCS metadata identifies local builds.
var BuildVersion = "dev"

func runBuildInfo(stdout io.Writer) int {
	info := map[string]any{"version": BuildVersion, "os": runtime.GOOS, "arch": runtime.GOARCH, "go_version": runtime.Version()}
	if executable, err := os.Executable(); err == nil {
		info["executable"] = executable
	}
	if build, ok := debug.ReadBuildInfo(); ok {
		for _, setting := range build.Settings {
			switch setting.Key {
			case "vcs.revision", "vcs.time", "vcs.modified":
				info[setting.Key] = setting.Value
			}
		}
	}
	return encodeJSON(stdout, info)
}
