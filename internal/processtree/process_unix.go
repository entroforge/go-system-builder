//go:build !windows

package processtree

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
	"time"
)

func configureCheckProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return os.ErrProcessDone
		}
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
}
func finishCheckProcess(cmd *exec.Cmd) {
	// A successful shell may still leave background descendants. They belong
	// to this check and must not outlive its receipt.
	if cmd.Process != nil {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}

func Run(cmd *exec.Cmd) error {
	if cmd.WaitDelay == 0 {
		cmd.WaitDelay = time.Second
	}
	configureCheckProcess(cmd)
	defer finishCheckProcess(cmd)
	return cmd.Run()
}
