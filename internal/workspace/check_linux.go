package workspace

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"time"
)

func runIsolatedCheck(ctx context.Context, main, common, worker, scratch, command string, output io.Writer) error {
	binary, err := exec.LookPath("bwrap")
	if err != nil {
		return fmt.Errorf("Worker checks require Bubblewrap with usable user namespaces: %w", err)
	}
	filter, err := checkSocketFilter()
	if err != nil {
		return err
	}
	defer os.Remove(filter.Name())
	defer filter.Close()
	// Keep the host network namespace because some containers prohibit the
	// NETLINK_ROUTE setup Bubblewrap performs for a new namespace. Seccomp
	// instead denies socket creation/connections and io_uring, including Unix
	// sockets which could otherwise reach host services through read-only paths.
	cmd := exec.CommandContext(ctx, binary, "--unshare-all", "--share-net", "--seccomp", "3", "--die-with-parent", "--new-session", "--ro-bind", "/", "/", "--dev", "/dev", "--proc", "/proc", "--tmpfs", "/tmp", "--tmpfs", "/run", "--ro-bind", main, main, "--ro-bind", common, common, "--bind", scratch, worker, "--chdir", worker, "--", "/bin/sh", "-c", command)
	cmd.ExtraFiles = []*os.File{filter}
	cmd.Stdout, cmd.Stderr = output, output
	cmd.WaitDelay = time.Second
	return cmd.Run()
}

func checkSocketFilter() (*os.File, error) {
	var arch uint32
	var blocked []uint32
	switch runtime.GOARCH {
	case "amd64":
		arch = 0xc000003e
		blocked = []uint32{41, 42, 53, 425, 426, 427}
	case "arm64":
		arch = 0xc00000b7
		blocked = []uint32{198, 199, 203, 425, 426, 427}
	default:
		return nil, fmt.Errorf("Worker check seccomp unsupported architecture %s", runtime.GOARCH)
	}
	// Classic BPF over seccomp_data: verify architecture, reject x32 and
	// socket/io_uring calls, then allow ordinary filesystem/build syscalls.
	type instruction struct {
		Code   uint16
		JT, JF uint8
		K      uint32
	}
	program := []instruction{{0x20, 0, 0, 4}, {0x15, 1, 0, arch}, {0x06, 0, 0, 0x80000000}, {0x20, 0, 0, 0}, {0x35, 0, 1, 0x40000000}, {0x06, 0, 0, 0x00050001}}
	for _, nr := range blocked {
		program = append(program, instruction{0x15, 0, 1, nr}, instruction{0x06, 0, 0, 0x00050001})
	}
	program = append(program, instruction{0x06, 0, 0, 0x7fff0000})
	f, err := os.CreateTemp("", "loop-worker-seccomp-")
	if err != nil {
		return nil, err
	}
	if err = binary.Write(f, binary.LittleEndian, program); err == nil {
		_, err = f.Seek(0, 0)
	}
	if err != nil {
		f.Close()
		os.Remove(f.Name())
		return nil, err
	}
	return f, nil
}
