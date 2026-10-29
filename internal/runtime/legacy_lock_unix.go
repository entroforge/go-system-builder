//go:build !windows

package runtime

import (
	"errors"
	"syscall"
)

// Permission errors and recycled PIDs are unknown/live, never proof of death.
func legacyOwnerExited(pid int) bool {
	return errors.Is(syscall.Kill(pid, 0), syscall.ESRCH)
}
