package e2erun

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
)

func currentPlatform() (Platform, error) {
	var host syscall.Utsname
	if err := syscall.Uname(&host); err != nil {
		return Platform{}, err
	}
	release := make([]byte, 0, len(host.Release))
	for _, b := range host.Release {
		if b == 0 {
			break
		}
		release = append(release, byte(b))
	}
	return Platform{OS: runtime.GOOS, Architecture: runtime.GOARCH, KernelRelease: string(release)}, nil
}

// The host-side isolation program is part of the platform boundary, not a
// project-selected executable. Node/config/spec code starts only inside it.
// A profile pins the approved bytes but cannot nominate another host command.
func validateIsolator(tool Tool) error {
	resolved, err := filepath.EvalSymlinks(tool.Path)
	if err != nil {
		return err
	}
	if resolved != "/usr/bin/bwrap" {
		return fmt.Errorf("isolation requires platform-owned /usr/bin/bwrap, got %s", resolved)
	}
	for p := resolved; ; p = filepath.Dir(p) {
		info, err := os.Stat(p)
		if err != nil {
			return err
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok || stat.Uid != 0 || info.Mode().Perm()&0022 != 0 {
			return fmt.Errorf("isolation path must be root-owned and not writable by other users: %s", p)
		}
		if p == "/" {
			break
		}
	}
	return nil
}
