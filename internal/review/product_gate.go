package review

import (
	"github.com/entroforge/go-system-builder/internal/buildsurface"
)

// FrozenSubjectsForProductBaseline is the existing pure machine projection,
// shared with the original S9 seed writer. It grants no source authority.
func FrozenSubjectsForProductBaseline(snapshot *buildsurface.Snapshot) ([]FrozenSubject, error) {
	return frozenSubjectsForProductBaseline(snapshot)
}
