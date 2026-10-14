//go:build !windows

package runtime

import (
	"errors"
	"syscall"
)

// Permission failures and PID reuse are deliberately not proof of death.
func processKnownAbsent(pid int) bool {
	return pid > 0 && errors.Is(syscall.Kill(pid, 0), syscall.ESRCH)
}
