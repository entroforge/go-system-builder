package processtree

import (
	"fmt"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

var jobDLL = syscall.NewLazyDLL("kernel32.dll")
var createJob = jobDLL.NewProc("CreateJobObjectW")
var setJob = jobDLL.NewProc("SetInformationJobObject")
var assignJob = jobDLL.NewProc("AssignProcessToJobObject")
var resumeProcess = syscall.NewLazyDLL("ntdll.dll").NewProc("NtResumeProcess")

type jobLimits struct {
	ProcessTime, JobTime                                       int64
	Flags                                                      uint32
	MinWorking, MaxWorking                                     uintptr
	ActiveProcesses                                            uint32
	Affinity                                                   uintptr
	Priority, Scheduling                                       uint32
	IO                                                         [6]uint64
	ProcessMemory, JobMemory, PeakProcessMemory, PeakJobMemory uintptr
}

// Start suspended so no child can escape before assignment to the kill-on-close
// Job Object. If assignment is unavailable, fail without executing the check.
func Run(cmd *exec.Cmd) error {
	if cmd.WaitDelay == 0 {
		cmd.WaitDelay = time.Second
	}
	h, _, e := createJob.Call(0, 0)
	if h == 0 {
		return fmt.Errorf("create check job: %w", e)
	}
	var once sync.Once
	closeJob := func() { once.Do(func() { _ = syscall.CloseHandle(syscall.Handle(h)) }) }
	defer closeJob()
	limits := jobLimits{Flags: 0x2000}
	if ok, _, e := setJob.Call(h, 9, uintptr(unsafe.Pointer(&limits)), unsafe.Sizeof(limits)); ok == 0 {
		return fmt.Errorf("configure check job: %w", e)
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x4}
	cmd.Cancel = func() error {
		closeJob()
		if cmd.Process == nil {
			return os.ErrProcessDone
		}
		return cmd.Process.Kill()
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	process, err := syscall.OpenProcess(0x901, false, uint32(cmd.Process.Pid))
	if err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return err
	}
	defer syscall.CloseHandle(process)
	if ok, _, e := assignJob.Call(h, uintptr(process)); ok == 0 {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return fmt.Errorf("assign check job: %w", e)
	}
	if status, _, _ := resumeProcess.Call(uintptr(process)); status != 0 {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return fmt.Errorf("resume check process: NTSTATUS %x", status)
	}
	return cmd.Wait()
}
