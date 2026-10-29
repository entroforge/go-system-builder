package workspace

import (
	"bytes"
	"context"
	"debug/elf"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/entroforge/go-system-builder/internal/assignmentrecord"
	"github.com/entroforge/go-system-builder/internal/checkexec"
)

func (b *ExecutionRegistry) observeCheckPermissions(ctx context.Context, e Execution, scratch string) checkexec.PermissionObservation {
	out := checkexec.PermissionObservation{Version: "1.0.0", Runner: "worker_bubblewrap_seccomp_v1", Platform: runtime.GOOS + "/" + runtime.GOARCH, UID: os.Geteuid(), GID: os.Getegid(), MainRoot: b.MainRoot, CommonDir: b.CommonDir, WorkerRoot: e.Path, CWD: e.Path}
	fail := func(err error) checkexec.PermissionObservation {
		out.Available = false
		out.Diagnostic = err.Error()
		return out
	}
	var err error
	out.Groups, err = os.Getgroups()
	if err != nil {
		return fail(err)
	}
	sort.Ints(out.Groups)
	kernel, err := os.ReadFile("/proc/sys/kernel/osrelease")
	if err != nil {
		return fail(err)
	}
	out.Kernel = strings.TrimSpace(string(kernel))
	binary, err := exec.LookPath("bwrap")
	if err != nil {
		return fail(err)
	}
	out.BinarySHA256, err = checkexec.FileDigest(binary)
	if err != nil {
		return fail(err)
	}
	out.ShellSHA256, err = checkexec.FileDigest("/bin/sh")
	if err != nil {
		return fail(err)
	}
	filter, err := checkSocketProgram()
	if err != nil {
		return fail(err)
	}
	out.FilterSHA256 = checkexec.Hash(filter)
	// Hash the actual argv producer with canonical disposable-copy coordinates.
	policy, _ := json.Marshal(isolatedCheckArguments(b.MainRoot, b.CommonDir, e.Path, "<disposable-copy>", "<original-declared-command>"))
	out.PolicySHA256 = checkexec.Hash(policy)
	authority, _ := json.Marshal(struct {
		Binding      checkexec.Binding
		Scope        []string
		Checks       []string
		WorkerChecks []assignmentrecord.WorkerCheck
		Inputs       map[string]string
		Bootstrap    string
	}{CheckBinding(e), e.WritePaths, e.Checks, e.WorkerChecks, e.Inputs, e.BootstrapSHA256})
	out.AuthoritySHA256 = checkexec.Hash(authority)
	if e.BootstrapSHA256 == "" {
		return fail(fmt.Errorf("original Worker bootstrap authority is missing"))
	}
	if err := b.ValidateBootstrap(e); err != nil {
		return fail(err)
	}
	p, ok := checkexec.CurrentProfile(ctx)
	if !ok || p.EnvironmentPolicy != "declared_only" {
		return fail(fmt.Errorf("inherited environment does not close isolated runner inputs"))
	}
	env, err := p.CommandEnvironment()
	if err != nil {
		return fail(err)
	}
	out.LibrariesSHA256, err = shellLibrariesDigest(ctx, env)
	if err != nil {
		return fail(err)
	}
	var diagnostic bytes.Buffer
	if err := runIsolatedCheck(ctx, b.MainRoot, b.CommonDir, e.Path, scratch, "true", &diagnostic); err != nil {
		message := strings.TrimSpace(diagnostic.String())
		if len(message) > 2048 {
			message = message[:2048]
		}
		return fail(fmt.Errorf("original isolation probe: %w: %s", err, message))
	}
	out.Available = true
	return out
}

// Use the ELF loader's read-only listing, under the exact declared environment,
// to hash the actual shell runtime, including its loader and shared libraries.
// Loader listing failures leave permissions unknown; they never enable reuse.
func shellLibrariesDigest(ctx context.Context, env []string) (string, error) {
	f, err := elf.Open("/bin/sh")
	if err != nil {
		return "", err
	}
	defer f.Close()
	libraries, err := f.ImportedLibraries()
	if err != nil {
		return "", err
	}
	loader := ""
	for _, prog := range f.Progs {
		if prog.Type == elf.PT_INTERP {
			data, err := io.ReadAll(io.LimitReader(prog.Open(), 4096))
			if err != nil {
				return "", err
			}
			loader = strings.TrimRight(string(data), "\x00")
		}
	}
	if loader == "" {
		if len(libraries) > 0 {
			return "", fmt.Errorf("shell dynamic loader is unknown")
		}
		return checkexec.Hash([]byte("static shell")), nil
	}
	if !filepath.IsAbs(loader) {
		return "", fmt.Errorf("shell loader is not absolute")
	}
	cmd := exec.CommandContext(ctx, loader, "--list", "/bin/sh")
	cmd.Env = env
	cmd.WaitDelay = time.Second
	listing, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("shell loader dependency listing: %w", err)
	}
	digests := map[string]string{}
	loaderDigest, err := checkexec.FileDigest(loader)
	if err != nil {
		return "", err
	}
	digests[loader] = loaderDigest
	resolved := map[string]bool{}
	for _, line := range strings.Split(string(listing), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 3 && fields[1] == "=>" && filepath.IsAbs(fields[2]) {
			digest, err := checkexec.FileDigest(fields[2])
			if err != nil {
				return "", err
			}
			digests[fields[2]] = digest
			resolved[fields[0]] = true
		}
	}
	for _, name := range libraries {
		if !resolved[name] {
			return "", fmt.Errorf("shell dependency is unresolved: %s", name)
		}
	}
	data, _ := json.Marshal(digests)
	return checkexec.Hash(data), nil
}
