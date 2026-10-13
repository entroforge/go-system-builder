//go:build !linux

package e2erun

import (
	"context"
	"fmt"
	"io"
)

func runIsolated(context.Context, string, []string, io.Writer, io.Writer) (int, error) {
	return -1, fmt.Errorf("controlled E2E execution requires the Linux Bubblewrap adapter; this platform has no supported private-network profile")
}

func currentPlatform() (Platform, error) {
	return Platform{}, fmt.Errorf("controlled E2E platform fingerprint is only implemented for Linux")
}

func validateIsolator(Tool) error { return fmt.Errorf("unsupported isolation platform") }
