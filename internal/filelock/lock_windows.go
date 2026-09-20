package filelock

import (
	"errors"
	"os"
	"syscall"
	"unsafe"
)

var kernel = syscall.NewLazyDLL("kernel32.dll")
var lockFile = kernel.NewProc("LockFileEx")
var unlockFile = kernel.NewProc("UnlockFileEx")

func tryLock(f *os.File) error {
	var ov syscall.Overlapped
	r, _, err := lockFile.Call(f.Fd(), 3, 0, 1, 0, uintptr(unsafe.Pointer(&ov)))
	if r == 0 {
		return err
	}
	return nil
}
func unlock(f *os.File) error {
	var ov syscall.Overlapped
	r, _, err := unlockFile.Call(f.Fd(), 0, 1, 0, uintptr(unsafe.Pointer(&ov)))
	if r == 0 {
		return err
	}
	return nil
}

func contended(err error) bool { return errors.Is(err, syscall.Errno(33)) }
