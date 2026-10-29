package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/repair"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

func TestNativeS9LocalEvidenceHandoffRetainsActualWorkerSource(t *testing.T) {
	root, worker, p, input, answer := nativeS9WorkerFixture(t)
	const log = ".claude/submissions/logs/check.txt"
	writeRepairLeadFile(t, root, log, "unrelated Main bytes")
	answer.EvidenceRefs = []string{log, "path:" + log, "test://actual-observation"}
	for i := range answer.UnitResults {
		answer.UnitResults[i].EvidenceRefs = []string{log}
	}
	resultPath := nativeS9TestResultPath(t, root, p)
	if err := persistRepairLeadQuestionnaire(filepath.Join(worker, resultPath), answer); err != nil {
		t.Fatal(err)
	}
	input.Event = "SubagentStop"
	var out, diagnostic bytes.Buffer
	// Main's same relative file is never a fallback for the missing Worker file.
	if _, code := nativeS9Hook(root, input, &out, &diagnostic); code != 0 || !strings.Contains(diagnostic.String(), "S9 local evidence source gap") || !strings.Contains(diagnostic.String(), worker) {
		t.Fatalf("missing source was not original recovery: %d %s %s", code, &out, &diagnostic)
	}
	writeRepairLeadFile(t, worker, log, "actual Worker observation")
	out.Reset()
	diagnostic.Reset()
	if _, code := nativeS9Hook(root, input, &out, &diagnostic); code != 0 {
		t.Fatalf("restored Worker source: %d %s %s", code, &out, &diagnostic)
	}
	snap := repairDispatchSnapshot(t, root)
	registry, err := workspace.Decode(snap.State)
	if err != nil {
		t.Fatal(err)
	}
	e := registry.Executions[p.Binding.ManifestAssignmentID]
	candidate, err := repair.ReadWorkspaceCandidate(root, repair.ArtifactRef{Path: e.DeliveryRef, SHA256: e.DeliverySHA256})
	if err != nil {
		t.Fatal(err)
	}
	if candidate.Provenance == nil || len(candidate.Provenance.LocalEvidence) != 3 {
		t.Fatalf("missing original receipt captures: %+v", candidate.Provenance)
	}
	submitted, _ := json.Marshal(answer)
	var canonical map[string]any
	if err := json.Unmarshal(submitted, &canonical); err != nil {
		t.Fatal(err)
	}
	submitted, _ = json.Marshal(canonical)
	if workpacket.Digest(submitted) != candidate.Provenance.AnswerSHA256 {
		t.Fatal("capture altered authored answer provenance")
	}
	if candidate.Proposal.EvidenceRefs[0] != log {
		t.Fatal("capture rewrote professional projection")
	}
	before := repairDispatchRuntimeBytes(t, root)
	out.Reset()
	diagnostic.Reset()
	if _, code := nativeS9Hook(root, input, &out, &diagnostic); code != 0 {
		t.Fatalf("accepted replay: %s", &diagnostic)
	}
	repairDispatchAssertUnchanged(t, root, before)
	out.Reset()
	diagnostic.Reset()
	if code := runWorkspaceDelivery([]string{"integrate", "--root", root, "--assignment", e.AssignmentID, "--agent", e.AgentID, "--check-command", "git grep -q 'const Restored = true' -- internal/api/one.go"}, &out, &diagnostic); code != 0 {
		t.Fatalf("Main original integration/Final: %d %s %s", code, &out, &diagnostic)
	}
	if _, err := os.Stat(worker); !os.IsNotExist(err) {
		t.Fatalf("Worker cleanup failed: %v", err)
	}
	for _, capture := range candidate.Provenance.LocalEvidence {
		data, err := workpacket.ReadFile(root, capture.Path, 20<<20)
		if err != nil {
			t.Fatal(err)
		}
		if capture.AuthoredRef == log || capture.AuthoredRef == "path:"+log {
			if string(data) != "actual Worker observation" {
				t.Fatalf("Main log substituted: %s", data)
			}
		}
	}
	out.Reset()
	diagnostic.Reset()
	if _, code := nativeS9Hook(root, input, &out, &diagnostic); code != 0 {
		t.Fatalf("late Stop after Worker cleanup: %s", &diagnostic)
	}
}
