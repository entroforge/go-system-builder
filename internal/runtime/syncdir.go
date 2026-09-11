package runtime

// SyncDirectory uses the platform's directory durability contract.
// Windows validates the directory; regular files are flushed separately.
func SyncDirectory(path string) error { return syncDir(path) }
