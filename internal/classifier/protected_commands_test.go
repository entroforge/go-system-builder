package classifier

import (
	"path/filepath"
	"strings"
	"testing"
)

// BUG-006 F1 regression: the four wrapper families (npx, bash, sh, env) must
// NOT use wildcard `.*` regex patterns that match every invocation. The
// tightened patterns match only the wrapper-form syntactic shape:
//   - npx: only `npx -- <cmd>` (literal `--` arg) is opaque to classifiers
//   - bash: only `bash -c <string>` (the `-c` flag) evaluates a string
//   - sh: same as bash
//   - env: row removed entirely; `env git push` is caught by the underlying
//     git-push row.
//
// Before F1, every `npx tsx test.ts` / `bash script.sh` / `env make test`
// was misclassified as a protected release operation, blocking normal
// development from the main session.
func TestBUG006F1WrapperPatternsAreNarrow(t *testing.T) {
	root := projectRoot(t)
	table, err := LoadProtectedCommands(root)
	if err != nil {
		t.Fatalf("load protected commands: %v", err)
	}

	// Helper: assert that the given command is or is not protected.
	assert := func(t *testing.T, command string, wantProtected bool) {
		t.Helper()
		resolved, err := Resolve(command)
		if err != nil {
			t.Fatalf("resolve %q: %v", command, err)
		}
		matched, reason, err := MatchProtectedCommands(resolved, table)
		if err != nil {
			t.Fatalf("match %q: %v", command, err)
		}
		if matched != wantProtected {
			t.Errorf("command=%q: got protected=%v want %v (reason=%q)",
				command, matched, wantProtected, reason)
		}
	}
	wantProtected := func(command string) func(*testing.T) {
		return func(t *testing.T) { assert(t, command, true) }
	}
	wantAllowed := func(command string) func(*testing.T) {
		return func(t *testing.T) { assert(t, command, false) }
	}

	t.Run("npx bare invocation allowed", wantAllowed("npx tsx test.ts"))
	t.Run("npx bare with redirect allowed", wantAllowed("npx tsx test.ts 2>&1 | tail -10"))
	t.Run("npx -- wrapper form blocked", wantProtected("npx -- evil-pkg"))
	t.Run("bash script.sh allowed", wantAllowed("bash script.sh"))
	t.Run("bash -c string blocked", wantProtected(`bash -c "git push origin main"`))
	t.Run("sh script.sh allowed", wantAllowed("sh script.sh"))
	t.Run("sh -c string blocked", wantProtected(`sh -c "git push origin main"`))
	t.Run("env make test allowed (env row removed)", wantAllowed("env make test"))
	// Known limitation (BUG-006 §5 F2): the classifier does not unwrap
	// wrapper programs, so `env git push origin main` parses as Program=env
	// and is no longer caught after the env wildcard row was removed. The
	// underlying git-push row keys on Program=git, not on the wrapped
	// command. F2 will add structural unwrap to the classifier; until then,
	// wrapper-form env passthroughs are an accepted gap. Bare `git push
	// origin main` (no env prefix) is still caught by the git-push row.
	t.Run("bare git push origin main blocked (no env prefix)", wantProtected("git push origin main"))

	// Regression: existing release operations still blocked.
	t.Run("npm publish blocked", wantProtected("npm publish"))
	t.Run("git push origin main blocked", wantProtected("git push origin main"))
	t.Run("git merge --squash main blocked", wantProtected("git merge --squash main"))
	t.Run("git tag v1.0.0 blocked", wantProtected("git tag v1.0.0"))

	// Regression: non-protected operations still allowed.
	t.Run("git push origin feature-branch allowed", wantAllowed("git push origin feature-branch"))
	t.Run("go test ./... allowed", wantAllowed("go test ./..."))
}

// projectRoot returns the repo root for test purposes. The classifier test
// package lives at internal/classifier/, so the repo root is two levels up.
func projectRoot(t *testing.T) string {
	t.Helper()
	abs, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	return abs
}

// Guard against accidental future re-widening: the table must NOT contain any
// row whose family is one of the four wrappers AND whose args_patterns uses a
// wildcard regex.
func TestBUG006F1NoWildcardWrapperRows(t *testing.T) {
	root := projectRoot(t)
	table, err := LoadProtectedCommands(root)
	if err != nil {
		t.Fatalf("load protected commands: %v", err)
	}
	wrapperFamilies := map[string]bool{"npx": true, "bash": true, "sh": true, "env": true}
	for _, row := range table {
		if !wrapperFamilies[row.Family] {
			continue
		}
		for _, p := range row.ArgsPatterns {
			if p.Regex == ".*" {
				t.Errorf("wildcard regex on family %q — BUG-006 regression: %q",
					row.Family, row.Reason)
			}
		}
		if row.Family == "env" {
			t.Errorf("env row present — BUG-006 F1 plan removed it; found: %q",
				strings.TrimSpace(row.Reason))
		}
	}
}
