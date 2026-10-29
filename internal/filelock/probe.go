package filelock

import (
	"errors"
	"fmt"
	"os"
)

// Probe observes an existing lock without creating or writing any file.
// Its result is a point-in-time resource observation, never an ownership grant.
func Probe(path string) (exists, busy bool, err error) {
	before, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, false, nil
	}
	if err != nil {
		return false, false, err
	}
	if !before.Mode().IsRegular() {
		return true, false, fmt.Errorf("lock observation requires an existing regular file")
	}
	f, err := os.Open(path)
	if err != nil {
		return true, false, err
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !os.SameFile(before, opened) {
		return true, false, fmt.Errorf("lock changed before observation")
	}
	lockErr := tryLock(f)
	if lockErr == nil {
		if err := unlock(f); err != nil {
			return true, false, err
		}
	} else if probeContended(lockErr) {
		busy = true
	} else {
		return true, false, lockErr
	}
	after, err := os.Lstat(path)
	if err != nil || !os.SameFile(opened, after) {
		return true, false, fmt.Errorf("lock changed during observation")
	}
	return true, busy, nil
}
