// Package runtimemeta contains the existing read-only Runtime snapshot DTO.
// It has no store, mutation, recovery or authority-producing operations.
package runtimemeta

type Snapshot struct {
	Revision int
	State    map[string]any
}
