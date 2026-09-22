//go:build !windows

package filelock

import (
	"errors"
	"os"
	"syscall"
)

func tryLock(f *os.File) error { return syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) }
func unlock(f *os.File) error  { return syscall.Flock(int(f.Fd()), syscall.LOCK_UN) }

func contended(err error) bool {
	return errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) || errors.Is(err, syscall.EINTR)
}
