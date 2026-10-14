//go:build !linux

package e2erun

import (
	"context"
	"testing"
)

func exerciseCapturedSIGKILL(t *testing.T, _ context.Context, _ Profile, _ string) {
	t.Skip("controlled adapter SIGKILL exercise requires Linux")
}
