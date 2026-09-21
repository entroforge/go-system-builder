package workspace

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/entroforge/go-system-builder/internal/processtree"
)

func NewSessionID() (string, error) {
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return "", err
	}
	id[6] = (id[6] & 15) | 64
	id[8] = (id[8] & 63) | 128
	return fmt.Sprintf("%x-%x-%x-%x-%x", id[:4], id[4:6], id[6:8], id[8:10], id[10:]), nil
}

// Launch changes only the child cwd. Identity must be persisted before calling;
// a retry must explicitly resume the same platform session, never guess latest.
func (b *ExecutionRegistry) Launch(ctx context.Context, e Execution, binary, role, prompt string, resume bool, stdout, stderr io.Writer) error {
	if err := b.Validate(ctx, b.MainRoot); err != nil {
		return err
	}
	if err := b.ValidateExecution(ctx, e, false); err != nil {
		return err
	}
	if err := b.ValidateInputs(e); err != nil {
		return err
	}
	if e.PlatformSessionID == "" || e.BootstrapSHA256 == "" || !safeID.MatchString(role) {
		return fmt.Errorf("launch requires persisted session identity, bootstrap and installed role")
	}
	rolePath := filepath.Join(e.Path, ".claude/agents", role+".md")
	if info, err := os.Stat(rolePath); err != nil || !info.Mode().IsRegular() {
		return fmt.Errorf("installed Worker role unavailable: %s", role)
	}
	flag := "--session-id"
	if resume {
		flag = "--resume"
	}
	contextData, _ := json.Marshal(map[string]any{"assignment": e, "observation_commands": ObservationCommands(e), "frozen_input_directory": ".claude/inputs", "begin_command": WorkerInvocation(e, "begin"), "commit_command": WorkerInvocation(e, "commit"), "report_command": WorkerInvocation(e, "report"), "s9_delivery_command": WorkerInvocation(e, "deliver")})
	prompt += "\nRegistered Harness context: " + string(contextData) + "\nRead approved input documents under .claude/inputs. Keep the Main checkout unchanged. Write lifecycle submissions under .claude/submissions: plan.json for begin, completion.json for report; use the repository agent-protocol schemas. commit.json requires expected_head, message and explicit paths. S9 repair-result.json is a candidate only; report then deliver. Use the observation_commands adapters for Git status, diffs and history; pwd is also allowed. Use only the exact registered adapters for other Bash; executable check commands are shown by workspace prepare. A process exit is not completion or release approval."
	args := []string{"--print", flag, e.PlatformSessionID, "--agent", role, "--settings", filepath.Join(e.Path, ".claude/settings.json"), "--setting-sources", "project", "--permission-mode", "dontAsk", "--allowedTools", "Read,Glob,Grep,Write,Edit,Bash", "--", prompt}
	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Dir = e.Path
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		// The new child has its own recorded identity. Do not inherit the parent's
		// nested-session marker; never copy or print credential values.
		if key == "CLAUDECODE" || key == "CLAUDE_CODE_SESSION_ID" {
			continue
		}
		cmd.Env = append(cmd.Env, entry)
	}
	return processtree.Run(cmd)
}

// SessionOwner recognizes only the registered top-level session in its own
// execution root. Native subagent IDs are validated separately by the caller.
func SessionOwner(ctx context.Context, main, cwd, session string) (string, error) {
	if session == "" {
		return "", nil
	}
	b, state, err := Load(main)
	if err != nil {
		return "", err
	}
	if b == nil {
		return "", nil
	}
	root, marked, err := PointerRoot(cwd)
	if err != nil {
		return "", err
	}
	if !marked {
		return "", nil
	}
	for _, e := range b.Executions {
		if e.Path != root || e.PlatformSessionID != session {
			continue
		}
		if e.Status != "ready" || e.RuntimeID != RuntimeID(state) || e.BaselineGeneration != Generation(state) {
			return "", fmt.Errorf("stale Worker platform session")
		}
		if err := b.ValidateExecution(ctx, e, false); err != nil {
			return "", err
		}
		return e.AgentID, nil
	}
	return "", fmt.Errorf("Worker platform session has no registered execution owner")
}
