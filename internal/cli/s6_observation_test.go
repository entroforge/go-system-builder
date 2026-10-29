package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/controller"
	req039fixtures "github.com/entroforge/go-system-builder/tests/fixtures/req039"
)

func TestS6UnchangedPreToolUseReusesObservation(t *testing.T) {
	root := req039fixtures.FreshRoot(t)
	state := req039fixtures.BaseState(t, root, "building", "", 19)
	req039fixtures.WriteState(t, root, state)
	runner := &req039fixtures.CLIRunner{}
	body := req039fixtures.PreToolUseBody("s6-observation", "Agent", map[string]any{"subagent_type": "s6-prior-delegation-marker"})
	run := func() string {
		t.Helper()
		code, out, errOut := req039fixtures.RunHook(t, runner, root, "PreToolUse", body)
		if code != 0 {
			t.Fatalf("PreToolUse: code=%d stderr=%s", code, errOut)
		}
		return out
	}
	// The ordinary wire omits ReadOrder and the policy audit envelope does
	// not carry Guidance. Check the original persisted role path, then use
	// the preflight Questions actually rendered by compactGuidance to detect
	// accidental reuse of the prior Agent's Guidance for a Main Read.
	first := run()
	initial := req039fixtures.ReadState(t, root)
	initialMilestone := initial["milestone"].(map[string]any)
	readOrder, err := json.Marshal(initialMilestone["read_order"])
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(readOrder, []byte(".claude/agents/s6-prior-delegation-marker.md")) {
		t.Fatal("initial Agent milestone did not include its delegation preflight")
	}
	if !strings.Contains(first, "Preflight questions:") {
		t.Fatal("initial Agent wire did not render its preflight Questions")
	}
	body = req039fixtures.PreToolUseBody("s6-observation", "Read", map[string]any{"file_path": "README.md"})
	var mainRead map[string]any
	if err := json.Unmarshal([]byte(body), &mainRead); err != nil {
		t.Fatal(err)
	}
	mainRead["agent_id"] = "Main"
	mainBody, err := json.Marshal(mainRead)
	if err != nil {
		t.Fatal(err)
	}
	body = string(mainBody)
	before := controller.MetricsGateEvaluations
	statePath := filepath.Join(root, ".claude/loop-state.json")
	journalPath := filepath.Join(root, ".claude/loop-events.jsonl")
	read := func(path string) []byte {
		t.Helper()
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	stateBytes, journalBytes := read(statePath), read(journalPath)
	for i := 0; i < 3; i++ {
		out := run()
		if strings.Contains(out, "Preflight questions:") {
			t.Fatal("prior Agent preflight leaked into the current Main Read")
		}
		gate, _ := req039fixtures.ParseQualityGate(t, out)
		if gate["transition_committed"] == true {
			t.Fatal("an existing observation was replayed as a new transition")
		}
	}
	if controller.MetricsGateEvaluations != before {
		t.Fatal("unchanged ordinary Read reran gate/source evaluation")
	}
	if !bytes.Equal(stateBytes, read(statePath)) || !bytes.Equal(journalBytes, read(journalPath)) {
		t.Fatal("unchanged ordinary Read rewrote Runtime or journal")
	}

	// A new revision with the same stage outcome must advance the existing
	// observation anchor once, then become read-only again.
	unchangedOutcome := req039fixtures.ReadState(t, root)
	unchangedOutcome["revision"] = req039fixtures.Revision(unchangedOutcome) + 1
	req039fixtures.WriteState(t, root, unchangedOutcome)
	run()
	observed := req039fixtures.ReadState(t, root)
	milestone := observed["milestone"].(map[string]any)
	if milestone["source_revision"] != observed["revision"] {
		t.Fatalf("same-outcome observation did not advance source_revision: %v / %v", milestone["source_revision"], observed["revision"])
	}
	before = controller.MetricsGateEvaluations
	stateBytes, journalBytes = read(statePath), read(journalPath)
	run()
	if controller.MetricsGateEvaluations != before || !bytes.Equal(stateBytes, read(statePath)) || !bytes.Equal(journalBytes, read(journalPath)) {
		t.Fatal("same-outcome observation did not settle after its new revision")
	}

	// Model a newly recorded batch through the existing fixture seeding
	// helper. Its new Runtime revision must defeat observation reuse.
	changed := req039fixtures.ReadState(t, root)
	req039fixtures.SeedBuilderBatchReady(t, root, changed)
	changed["revision"] = req039fixtures.Revision(changed) + 1
	req039fixtures.WriteState(t, root, changed)
	run()
	if controller.MetricsGateEvaluations == before {
		t.Fatal("changed Runtime facts did not execute the original gate")
	}
	if lifecycle, phase := req039fixtures.Lifecycle(req039fixtures.ReadState(t, root)); lifecycle != "verification" || phase != "planned" {
		t.Fatalf("changed ready batch did not advance through TR-006: %s/%s", lifecycle, phase)
	}
}
