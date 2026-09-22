package integration

import (
	"github.com/entroforge/go-system-builder/internal/processtree"
	"os/exec"
)

func runCheckProcess(cmd *exec.Cmd) error { return processtree.Run(cmd) }
