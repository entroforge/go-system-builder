package checkexec

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"unicode"
)

type Entrance struct {
	Command    string   `json:"command"`
	Status     string   `json:"status"`
	Tool       string   `json:"tool,omitempty"`
	Executable string   `json:"executable,omitempty"`
	Scope      string   `json:"verification_scope"`
	Reason     string   `json:"reason,omitempty"`
	Failure    *Failure `json:"failure,omitempty"`
}
type CapabilityReport struct {
	Runner        string     `json:"runner"`
	Available     bool       `json:"available"`
	FullyVerified bool       `json:"fully_verified"`
	Shell         string     `json:"shell,omitempty"`
	Checks        []Entrance `json:"checks"`
}

// LiteralEntrance accepts only simple shell argv, without expansion or control
// syntax. It does not run any command or treat opaque syntax as ready evidence.
func LiteralEntrance(command string) (string, bool) {
	words, ok := LiteralArguments(command)
	if !ok {
		return "", false
	}
	if words[0] == "exec" {
		if len(words) < 2 {
			return "", false
		}
		return words[1], true
	}
	return words[0], true
}

// LiteralArguments is the same bounded shell syntax classifier as entrance
// preflight. It parses literals only and never performs shell expansion.
func LiteralArguments(command string) ([]string, bool) {
	command = strings.TrimSpace(command)
	words := []string{}
	var word strings.Builder
	quote := rune(0)
	active := false
	for _, c := range command {
		if c == '\n' || c == '\r' || c == 0 {
			return nil, false
		}
		if quote != 0 {
			if c == quote {
				quote = 0
				continue
			}
			if quote == '"' && strings.ContainsRune("$`\\", c) {
				return nil, false
			}
			word.WriteRune(c)
			active = true
			continue
		}
		if c == '\'' || c == '"' {
			quote = c
			active = true
			continue
		}
		if unicode.IsSpace(c) {
			if active {
				words = append(words, word.String())
				word.Reset()
				active = false
			}
			continue
		}
		if strings.ContainsRune("|&;<>$`(){}\\#", c) {
			return nil, false
		}
		word.WriteRune(c)
		active = true
	}
	if quote != 0 {
		return nil, false
	}
	if active {
		words = append(words, word.String())
	}
	if len(words) == 0 || words[0] == "" || strings.Contains(words[0], "=") {
		return nil, false
	}
	return words, true
}

var builtins = map[string]bool{"true": true, "false": true, "test": true, "[": true, "printf": true, "echo": true, "pwd": true, ":": true, "exit": true, "cd": true, "read": true, "umask": true, "export": true, "unset": true, "set": true, "command": true}

// MainCapabilities probes only the actual interpreter/executable entrance.
// FullyVerified stays false until an explicit dependency/resource profile exists.
func MainCapabilities(ctx context.Context, root string, commands []string) CapabilityReport {
	out := CapabilityReport{Runner: "main_shell", Available: true, FullyVerified: false, Checks: []Entrance{}}
	required := false
	for _, command := range commands {
		if !strings.HasPrefix(strings.TrimSpace(command), "locked:") {
			required = true
		}
	}
	if !required {
		return out
	}
	shell, err := exec.LookPath("sh")
	if err == nil {
		out.Shell = shell
	} else {
		out.Available = false
	}
	for _, command := range commands {
		if strings.HasPrefix(strings.TrimSpace(command), "locked:") {
			continue
		}
		row := Entrance{Command: command, Status: "unverified", Scope: "interpreter/executable entrance only; dependencies, configuration, fixtures and services not verified"}
		fail := func(err error) {
			row.Status = "unavailable"
			row.Failure = New("capability_missing", "environment owner", command, err)
			out.Available = false
		}
		if ctx.Err() != nil {
			row.Failure = Classify(ctx, "environment owner", command, ctx.Err())
			row.Status = "unavailable"
			out.Available = false
		} else if err != nil {
			fail(fmt.Errorf("Main shell is unavailable: %w", err))
		} else if tool, ok := LiteralEntrance(command); !ok {
			row.Reason = "legacy shell syntax has no proven static tool closure; use an explicit approved check profile for complete readiness"
		} else {
			row.Tool = tool
			if builtins[tool] {
				row.Status = "entrance_available"
				row.Executable = shell
			} else {
				path := tool
				var lookupErr error
				if strings.Contains(tool, "/") || filepath.IsAbs(tool) {
					if !filepath.IsAbs(path) {
						path = filepath.Join(root, path)
					}
					path, lookupErr = exec.LookPath(path)
				} else {
					path, lookupErr = exec.LookPath(tool)
				}
				if lookupErr != nil {
					fail(fmt.Errorf("declared check tool %s unavailable: %w", tool, lookupErr))
				} else {
					row.Status = "entrance_available"
					row.Executable = path
				}
			}
		}
		out.Checks = append(out.Checks, row)
	}
	return out
}
func (r CapabilityReport) Error() error {
	for _, row := range r.Checks {
		if row.Failure != nil {
			return row.Failure
		}
	}
	return nil
}
