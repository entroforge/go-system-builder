package runtime

import (
	"errors"
	"syscall"
)

func legacyOwnerExited(pid int) bool {
	handle, err := syscall.OpenProcess(0x1000, false, uint32(pid))
	if err != nil {
		return errors.Is(err, syscall.Errno(87)) // no such process
	}
	defer syscall.CloseHandle(handle)
	var code uint32
	return syscall.GetExitCodeProcess(handle, &code) == nil && code != 259 // STILL_ACTIVE
}
