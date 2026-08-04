// Package classifier: command.go provides the legacy string-prefix
// ClassifyCommand API plus a thin dispatcher that routes through the new
// tokenized resolver. New programs and any path-aware logic live in
// resolve.go and bash_tokenize.go; this file remains the entrypoint the
// policy engine calls and must keep returning sensible Class values for the
// existing call sites.
//
// Family list covered (BUG-002 §4b.2(b) lines 165-176):
//
//	git, sed, awk, perl, ed, gh, npm, goreleaser, terraform, kubectl, aws,
//	plus the existing go/cargo/pytest/eslint/lint families.
//
// Tokenize() (per BUG-002 §4b.2(b) line 146): unbalanced quotes/redirects/
// parens return an error; the resolver surfaces that as an "unknown"
// classification so the policy engine can fail closed.
package classifier

import (
	"fmt"
	"strings"
)

// CommandResult is the classification returned by ClassifyCommand. Existing
// fields (Class, Unknown) are preserved so the policy engine's scope-violation
// predicate keeps working unchanged.
type CommandResult struct {
	Class   string
	Unknown bool
	// Resolved is the structured ResolvedCommand when tokenization
	// succeeded. It is non-nil whenever Tokenize() did not return an error
	// AND the program is in the resolver family list. Existing callers that
	// only need Class/Unknown can ignore it.
	Resolved *ResolvedCommand
}

// ClassifyCommand classifies a bash command into one of the Loop's known
// command classes. It preserves the existing public contract: returns
// CommandResult{Class, Unknown}. The new Resolved field carries the
// tokenized/resolved view that the policy engine consumes for path-aware
// predicates (HS-004) and protected-release matching (HS-005).
//
// Behavioural changes from the prior implementation:
//
//   - Programs in the resolver family list (git, sed, awk, perl, ed, gh, npm,
//     goreleaser, terraform, kubectl, aws, go) now route through Resolve()
//     so path-affecting flags (-i, --in-place) and protected-release
//     subcommands are recognised correctly.
//   - Shell-composition characters (&&, ||, ;, |, >, <, $(, `) no longer
//     flatten to "unknown" — instead the resolver inspects each tokenized
//     command in turn. If the program is unknown AND any operator is
//     present, the classification is "unknown" (fail-closed per BUG-002
//     §4b.2(b) line 178).
//   - Unbalanced quotes/redirects/parens (a Tokenize() error) collapse to
//     Class: "unknown", Unknown: true with the error message attached via
//     the Unknown field; callers should treat Unknown==true as deny.
func ClassifyCommand(command string) CommandResult {
	if command == "" {
		return CommandResult{Class: "unknown", Unknown: true}
	}

	resolved, err := Resolve(command)
	if err != nil {
		// Unbalanced quotes/redirects/parens or any tokenizer error → unknown.
		return CommandResult{Class: "unknown", Unknown: true}
	}

	// Operator-heavy composition (e.g. `cmd1 && cmd2`) collapses to
	// "unknown" for classification purposes — the resolver still records
	// the affected paths of the first command, which downstream predicates
	// use. Activated-subagent scope-violation checks will deny on
	// Unknown==true when "unknown" is not in the agent's allowed list.
	if HasOperator(resolved.Args) {
		for _, token := range resolved.Args {
			if token.Kind == TkRedirect {
				return CommandResult{Class: "unknown", Unknown: true, Resolved: &resolved}
			}
		}
		// Allow known programs through — `git tag && git push` must still
		// be recognised so HS-005 catches it.
		if !isKnownProgram(resolved.Program) {
			return CommandResult{
				Class:    "unknown",
				Unknown:  true,
				Resolved: &resolved,
			}
		}
	}

	// Map family → class. Unknown programs return "unknown".
	if resolved.Family == "" {
		if isReadOnlyProgram(resolved.Program) {
			return CommandResult{
				Class:    "read_only",
				Unknown:  false,
				Resolved: &resolved,
			}
		}
		return CommandResult{
			Class:    "unknown",
			Unknown:  true,
			Resolved: &resolved,
		}
	}
	class, ok := familyClass(resolved)
	if !ok {
		return CommandResult{
			Class:    "unknown",
			Unknown:  true,
			Resolved: &resolved,
		}
	}
	return CommandResult{
		Class:    class,
		Unknown:  false,
		Resolved: &resolved,
	}
}

// isKnownProgram reports whether the program name has a registered resolver.
func isKnownProgram(program string) bool {
	switch program {
	case "git", "sed", "awk", "perl", "ed",
		"gh", "npm", "goreleaser",
		"terraform", "kubectl", "aws",
		"go", "cargo", "pytest", "eslint", "golangci-lint", "rg":
		return true
	}
	return false
}

// isReadOnlyProgram reports whether the program name has a read-only
// classification (ls, cat, echo, rg, etc.). These programs never appear in
// the protected-commands table but should not be flagged as "unknown" —
// the policy engine's scope-violation check only fires when the agent's
// allowed-command-classes does not include the resulting class.
func isReadOnlyProgram(program string) bool {
	switch program {
	case "ls", "cat", "echo", "rg", "grep", "head", "tail", "wc", "find",
		"pwd", "tree", "file", "stat":
		return true
	}
	return false
}

// IsReadOnlyProgram is the exported version of isReadOnlyProgram used by the
// policy engine's HS-012 predicate. Activated subagents are allowed to
// invoke these programs; the agent's AllowedCommandClasses still applies for
// ordinary scope checks.
func IsReadOnlyProgram(program string) bool {
	return isReadOnlyProgram(program)
}

// familyClass maps a resolved command to its Loop command class. The class
// is what the policy engine's scope-violation predicate compares against
// agent.AllowedCommandClasses.
func familyClass(r ResolvedCommand) (string, bool) {
	switch r.Family {
	case "go":
		switch r.Subcommand {
		case "test":
			return "test", true
		case "build":
			return "build", true
		case "vet":
			return "lint", true
		}
		return "read_only", true
	case "git":
		// The protected-commands table (BUG-002 §4b.2(d)) classifies
		// release-shaped git operations as protected_release. Mapping git
		// push/merge --squash/tag to "protected_release" here means
		// HS-005's protected_release_command predicate wins over HS-003's
		// scope_violation (the agent must not have "protected_release" in
		// AllowedCommandClasses, and HS-003's unknown-class path is not
		// triggered). Other mutating subcommands (commit, apply, checkout,
		// restore) map to "git" — path-aware HS-004 still gates these.
		switch r.Subcommand {
		case "push", "tag":
			return "protected_release", true
		case "merge":
			if hasAnyFlag(r.Args, []string{"--squash"}) {
				return "protected_release", true
			}
			return "git", true
		case "commit", "apply", "checkout", "restore":
			return "git", true
		}
		return "read_only", true
	case "sed", "awk", "perl", "ed":
		if r.Mutates {
			return "git", true // mutating text-edit families map to git (path-mutating) class
		}
		return "read_only", true
	case "gh":
		switch {
		case strings.HasPrefix(r.Subcommand, "release "):
			return "protected_release", true
		case strings.HasPrefix(r.Subcommand, "pr ") && strings.HasSuffix(r.Subcommand, " merge"):
			return "protected_release", true
		}
		return "git", true
	case "npm":
		switch r.Subcommand {
		case "publish":
			return "protected_release", true
		case "install", "uninstall":
			return "dependency_mutation", true
		case "test":
			return "test", true
		case "run":
			// `npm run lint` → lint class.
			return "lint", true
		}
		return "git", true
	case "goreleaser":
		if r.Subcommand == "release" {
			return "protected_release", true
		}
		return "git", true
	case "terraform":
		if r.Mutates {
			return "git", true
		}
		return "read_only", true
	case "kubectl":
		if r.Mutates {
			return "git", true
		}
		return "read_only", true
	case "aws":
		if r.Mutates {
			return "git", true
		}
		return "read_only", true
	}
	return "", false
}

// String renders a human-readable summary for debug output.
func (r CommandResult) String() string {
	if r.Unknown {
		return fmt.Sprintf("unknown(class=unknown)")
	}
	return fmt.Sprintf("class=%s", r.Class)
}
