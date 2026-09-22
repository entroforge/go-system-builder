package workspace

import (
	"context"
	"fmt"
	"github.com/entroforge/go-system-builder/internal/processtree"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"
)

var observationViews = []string{"cwd", "status", "diff", "staged", "log"}

func ObservationCommands(e Execution) map[string]string {
	commands := map[string]string{}
	for _, view := range observationViews {
		commands[view] = WorkerInvocation(e, "observe") + " --view " + view
	}
	return commands
}

// ValidateObservationEnvironment rejects inherited Git overrides before even
// routing/identity probes can invoke Git. Pager/diff helpers are separately
// disabled; all other Git overrides must be cleared by the invoking environment.
func ValidateObservationEnvironment() error {
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		key = strings.ToUpper(key)
		if strings.HasPrefix(key, "GIT_") && key != "GIT_PAGER" && key != "GIT_EXTERNAL_DIFF" {
			return fmt.Errorf("observation requires clearing inherited %s before Git identity checks", key)
		}
	}
	return nil
}

// Observe accepts a view, never caller-supplied Git flags, paths or shell code.
func (b *ExecutionRegistry) Observe(ctx context.Context, e Execution, view string, output io.Writer) error {
	if err := ValidateObservationEnvironment(); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if e.Status != "ready" {
		return fmt.Errorf("observation requires a ready execution")
	}
	if err := b.Validate(ctx, b.MainRoot); err != nil {
		return err
	}
	if err := b.ValidateExecution(ctx, e, false); err != nil {
		return err
	}
	if err := b.ValidateInputs(e); err != nil {
		return err
	}
	if err := b.ValidateBootstrap(e); err != nil {
		return err
	}
	args := []string{"--no-pager", "-c", "core.fsmonitor=false", "-c", "core.untrackedCache=false", "-c", "gc.auto=0", "-c", "maintenance.auto=false"}
	switch view {
	case "cwd":
		_, err := fmt.Fprintln(output, e.Path)
		return err
	case "status":
		args = append(args, "status", "--short", "--untracked-files=normal", "--ignore-submodules=all")
	case "diff":
		args = append(args, "diff", "--no-ext-diff", "--no-textconv", "--ignore-submodules=all", "--stat")
	case "staged":
		args = append(args, "diff", "--cached", "--no-ext-diff", "--no-textconv", "--ignore-submodules=all", "--stat")
	case "log":
		args = append(args, "log", "--no-ext-diff", "--no-textconv", "--no-show-signature", "-20", "--oneline")
	default:
		return fmt.Errorf("unknown observation view; choose cwd, status, diff, staged or log")
	}
	binary, err := exec.LookPath("git")
	if err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Dir = e.Path
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(strings.ToUpper(key), "GIT_") {
			continue
		}
		cmd.Env = append(cmd.Env, entry)
	}
	cmd.Env = append(cmd.Env, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_SYSTEM="+os.DevNull, "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_OPTIONAL_LOCKS=0", "GIT_TERMINAL_PROMPT=0", "GIT_NO_LAZY_FETCH=1", "GIT_PAGER=cat")
	bounded := &observationOutput{writer: output, remaining: 1 << 20}
	cmd.Stdout = bounded
	cmd.Stderr = bounded
	cmd.WaitDelay = time.Second
	return processtree.Run(cmd)
}

type observationOutput struct {
	writer    io.Writer
	remaining int
}

func (o *observationOutput) Write(p []byte) (int, error) {
	if len(p) > o.remaining {
		return 0, fmt.Errorf("observation exceeds 1 MiB; use scoped file tools")
	}
	n, err := o.writer.Write(p)
	o.remaining -= n
	return n, err
}
