//go:build !linux

package workspace

import (
	"context"
	"fmt"
	"io"
)

func runIsolatedCheck(context.Context, string, string, string, string, string, io.Writer) error {
	return fmt.Errorf("isolated Worker checks are unavailable on this platform; run project checks from Main without disabling Worker boundaries")
}
