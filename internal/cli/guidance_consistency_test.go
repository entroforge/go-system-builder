package cli_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"unicode"
)

// TestGuidanceVerbsExistInRuntime keeps the guidance chain honest: every
// `runtime <verb>` and `loop-harness <verb>` named in an agent card, skill,
// or the examples README must be a real CLI subcommand. When a verb is
// renamed the docs fail here first instead of at a live agent's submission
// (the round-14 "why does friction keep recurring" close-out: drift belongs
// in CI, not in the next review round).
func TestGuidanceVerbsExistInRuntime(t *testing.T) {
	root := filepath.Join("..", "..")
	usage, err := os.ReadFile(filepath.Join(root, "internal", "cli", "run.go"))
	if err != nil {
		t.Fatal(err)
	}
	known := map[string]bool{}
	for _, m := range regexp.MustCompile(`"([a-z7-]+)"`).FindAllStringSubmatch(string(usage), -1) {
		known[m[1]] = true
	}
	// subverbs live in their command files
	for _, file := range []string{"investigation_command.go", "repair_command.go"} {
		data, err := os.ReadFile(filepath.Join(root, "internal", "cli", file))
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range regexp.MustCompile(`args\[0\] != "([a-z-]+)"`).FindAllStringSubmatch(string(data), -1) {
			known[m[1]] = true
		}
		for _, m := range regexp.MustCompile(`=="([a-z-]+)"`).FindAllStringSubmatch(string(data), -1) {
			known[m[1]] = true
		}
	}
	surfaces := []string{
		filepath.Join(root, "agents", "investigator.md"),
		filepath.Join(root, "agents", "backend-builder.md"),
		filepath.Join(root, "skills", "bug-resolution", "SKILL.md"),
		filepath.Join(root, "skills", "agent-dispatch", "SKILL.md"),
		filepath.Join(root, "docs", "examples", "s7-s9", "README.md"),
	}
	verbRe := regexp.MustCompile("`(?:loop-harness )?runtime ([a-z7-]+)")
	for _, surface := range surfaces {
		data, err := os.ReadFile(surface)
		if err != nil {
			t.Fatalf("%s: %v", surface, err)
		}
		for _, m := range verbRe.FindAllStringSubmatch(string(data), -1) {
			verb := m[1]
			switch verb {
			case "transition", "reconcile", "evidence", "status", "next", "ready", "doctor":
				known[verb] = known[verb] // top-level, always present
			}
			if !known[verb] {
				t.Errorf("%s references `runtime %s` but no such subcommand exists in the CLI", filepath.Base(surface), verb)
			}
		}
	}
}

// TestProtocolTransitionIdsResolvable pins that transition ids the protocol
// leans on are either catalog TRs (present in loop-definition) or documented
// runtime-authority ids (manual item 11).
func TestProtocolTransitionIdsResolvable(t *testing.T) {
	root := filepath.Join("..", "..")
	def, err := os.ReadFile(filepath.Join(root, "docs", "loop-definition.json"))
	if err != nil {
		t.Fatal(err)
	}
	proto, err := os.ReadFile(filepath.Join(root, "docs", "agent-protocol.md"))
	if err != nil {
		t.Fatal(err)
	}
	runtimeIDs := map[string]bool{
		"S8-REPAIR-CONTRACT-APPROVAL": true,
		"REVIEW-RESULT":               true,
		"REVIEW-PLAN-STALE":           true,
		"S7-BUDGET-DECISION":          true,
		"AGENT-LIFECYCLE":             true,
		"BUG-LIFECYCLE":               true,
		"EVIDENCE-RECORD":             true,
	}
	for _, m := range regexp.MustCompile(`\b([A-Z][A-Z0-9-]{3,}-[A-Z0-9-]*[0-9])\b|\b(S8-REPAIR-CONTRACT-APPROVAL)\b`).FindAllStringSubmatch(string(proto), -1) {
		id := m[1]
		if id == "" {
			id = m[2]
		}
		if runtimeIDs[id] {
			continue
		}
		if !strings.Contains(string(def), `"id": "`+id+`"`) && !strings.Contains(string(def), id) {
			t.Errorf("protocol references %q which is neither in loop-definition nor a documented runtime-authority id", id)
		}
	}
}

// TestAgentProtocolStaysRunbookScoped prevents intermediate audit material from
// being promoted into the Main Spine again. The protocol is an English
// constitution/route table; S7-S9 control-plane detail is owned by the L3/L4
// documents and must not return as a protocol anchor.
func TestAgentProtocolStaysRunbookScoped(t *testing.T) {
	root := filepath.Join("..", "..")
	path := filepath.Join(root, "docs", "agent-protocol.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range string(data) {
		if unicode.Is(unicode.Han, r) {
			t.Fatalf("%s contains Han character %q; keep the Main Spine in English", path, r)
		}
	}
	if strings.Contains(string(data), "s7s9-control-plane-map") {
		t.Fatalf("%s contains the removed S7-S9 control-plane map anchor; link the owning L3/L4 document instead", path)
	}
}
