package workspace

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"sort"
	"strings"

	"github.com/entroforge/go-system-builder/internal/agentcontext"
	"github.com/entroforge/go-system-builder/internal/assignmentrecord"
	"github.com/entroforge/go-system-builder/internal/checkexec"
)

func observeNativeCheckPermissions(ctx context.Context, root, common, checkout, scratch string, req NativeOwnedCheckRequest) checkexec.PermissionObservation {
	out := checkexec.PermissionObservation{Version: "1.0.0", Runner: "worker_bubblewrap_seccomp_v1", Platform: runtime.GOOS + "/" + runtime.GOARCH, UID: os.Geteuid(), GID: os.Getegid(), MainRoot: root, CommonDir: common, WorkerRoot: checkout, CWD: checkout}
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
	policy, _ := json.Marshal(isolatedCheckArguments(root, common, checkout, "<disposable-copy>", "<original-declared-command>"))
	out.PolicySHA256 = checkexec.Hash(policy)
	authority, _ := json.Marshal(struct {
		Binding           checkexec.Binding
		Record            assignmentrecord.RecordReference
		RecordSHA         string
		ContextGeneration int
		ContextRef        agentcontext.Reference
		ContextSHA        string
		ActivationRef     string
		ActivationSHA     string
		Check             assignmentrecord.WorkerCheck
		Source            NativeCheckSource
	}{checkexec.Binding{RuntimeID: req.RuntimeID, Generation: req.BaselineGeneration, AssignmentID: req.AssignmentID, Owner: req.OwnerAgentID, Responsibility: "owned_check"}, req.Record, req.RecordSHA256, req.ContextGeneration, req.ContextRef, req.ContextSHA256, req.ActivationRef, req.ActivationSHA256, req.Check, req.Source})
	out.AuthoritySHA256 = checkexec.Hash(authority)
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
	var diagnostic strings.Builder
	if err := runIsolatedCheck(ctx, root, common, checkout, scratch, "true", &diagnostic); err != nil {
		message := diagnostic.String()
		if len(message) > 2048 {
			message = message[:2048]
		}
		return fail(fmt.Errorf("original isolation probe: %w: %s", err, message))
	}
	out.Available = true
	return out
}
