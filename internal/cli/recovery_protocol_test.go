package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/cli"
)

func TestRecoveryCLIInspectsButCannotReconstructNewProtocol(t *testing.T) {
	root := newRecoveryCommandRoot(t)
	commitStageFixture(t, root)
	statePath := filepath.Join(root, ".claude/loop-state.json")
	state := []byte(`{"schema_version":"2.0.0","active_operations":{},"protocol_capabilities":[]}`)
	if err := os.WriteFile(statePath, state, 0o644); err != nil {
		t.Fatal(err)
	}
	journalPath := filepath.Join(root, ".claude/loop-events.jsonl")
	journal, err := os.ReadFile(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, verb := range []string{"inspect", "plan", "apply"} {
		args := []string{"runtime", "recover", verb, "--root", root}
		if verb == "apply" {
			// The Runtime barrier must run before reading a caller's old plan.
			args = append(args, "--plan", "unused-legacy-plan.json", "--approved-by", "fixture-human")
		} else {
			args = append(args, "--req", "docs/requirements/REQ-900.md")
			if verb == "plan" {
				args = append(args, "--dev-branch", "test-development", "--release-upstream", "origin/release")
			}
		}
		var stdout, stderr bytes.Buffer
		code := cli.Run(args, strings.NewReader(""), &stdout, &stderr)
		if verb == "inspect" {
			if code != 0 {
				t.Fatalf("inspect should remain available: %s", stderr.String())
			}
		} else if code == 0 || !strings.Contains(stderr.String(), "LOOP_RECOVERY_PROTOCOL_UNSUPPORTED") {
			t.Fatalf("%s: code=%d stderr=%s", verb, code, stderr.String())
		}
		actualState, _ := os.ReadFile(statePath)
		actualJournal, _ := os.ReadFile(journalPath)
		if !bytes.Equal(actualState, state) || !bytes.Equal(actualJournal, journal) {
			t.Fatalf("%s changed the active pair", verb)
		}
		if _, err := os.Stat(filepath.Join(root, ".claude/recovery")); !os.IsNotExist(err) {
			t.Fatalf("%s created a recovery plan/quarantine: %v", verb, err)
		}
	}
}

func TestLifecycleCLIRejectsUnknownProtocolBeforeWritingDecision(t *testing.T) {
	for _, args := range [][]string{
		{"runtime", "pause", "--reason", "fixture", "--approved-by", "fixture-human"},
		{"req", "unbind", "--reason", "fixture", "--approved-by", "fixture-human"},
	} {
		t.Run(strings.Join(args[:2], "-"), func(t *testing.T) {
			root := newRecoveryCommandRoot(t)
			statePath := filepath.Join(root, ".claude/loop-state.json")
			state := []byte(`{"schema_version":"2.0.0","runtime_id":"fixture","revision":0,"lifecycle":{"state":"planning","phase":"design"},"journal":{"last_sequence":0,"last_event_id":null}}`)
			if err := os.WriteFile(statePath, state, 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, ".claude/loop-events.jsonl"), nil, 0o644); err != nil {
				t.Fatal(err)
			}
			var stdout, stderr bytes.Buffer
			code := cli.Run(append(args, "--root", root), strings.NewReader(""), &stdout, &stderr)
			if code == 0 || !strings.Contains(stderr.String(), "unsupported or invalid Runtime") {
				t.Fatalf("code=%d stderr=%s", code, stderr.String())
			}
			if _, err := os.Stat(filepath.Join(root, ".claude/decisions")); !os.IsNotExist(err) {
				t.Fatalf("rejected protocol left a decision artifact: %v", err)
			}
			actual, _ := os.ReadFile(statePath)
			if !bytes.Equal(actual, state) {
				t.Fatal("rejected lifecycle command changed state")
			}
		})
	}
}
