package runtime

import (
	"fmt"
	"os"
)

// Windows does not provide POSIX fsync semantics on os.Open directory handles.
// Regular files are still Sync'ed by the writer and recovery pending markers
// still protect interrupted processes. Directory-entry durability across power
// loss is NOT guaranteed here; see docs/runtime-portability.md.
func syncDir(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("sync directory: %s is not a directory", path)
	}
	return nil
}
