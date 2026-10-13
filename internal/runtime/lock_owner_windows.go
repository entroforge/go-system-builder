package runtime

import (
	"errors"
	"syscall"
)

func processKnownAbsent(pid int) bool {
	if pid <= 0 || uint64(pid) > uint64(^uint32(0)) {
		return false
	}
	// PROCESS_QUERY_LIMITED_INFORMATION; no termination or write access.
	handle, err := syscall.OpenProcess(0x1000, false, uint32(pid))
	if err != nil {
		return errors.Is(err, syscall.Errno(87)) // ERROR_INVALID_PARAMETER: no such PID.
	}
	defer syscall.CloseHandle(handle)
	var exitCode uint32
	return syscall.GetExitCodeProcess(handle, &exitCode) == nil && exitCode != 259 // STILL_ACTIVE
}
