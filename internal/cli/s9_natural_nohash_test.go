package cli

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/policy"
	"github.com/entroforge/go-system-builder/internal/schema"
)

func TestNativeS9DiagnosticObservesResponsibilityWithoutDigest(t *testing.T) {
	root := t.TempDir()
	snapshot := attemptDeliverySnapshot()
	key := strings.Repeat("long-original-responsibility-", 15)
	rows := objectSliceAny(mapFieldCLI(snapshot.State, "entities")["work_packets"])
	rows[0]["packet_id"] = key
	capture := newNativeS9AttemptCapture(context.Background(), root, snapshot, key, "builder-original", "Builder")
	capture.ObserveInput("actual-answer.json", []byte("private professional bytes"))
	capture.SetStage("plan_report_consumer")
	if err := capture.Publish(context.Background(), errors.New("actual consumer failure")); err != nil {
		t.Fatal(err)
	}
	path := nativeS9DiagnosticPath(key)
	for _, part := range strings.Split(path, "/") {
		if len(part) > 120 {
			t.Fatalf("diagnostic path component exceeds its bounded split: %d", len(part))
		}
	}
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "private professional bytes") || strings.Contains(strings.ToLower(string(data)), "sha256") {
		t.Fatalf("diagnostic copied professional input or digest metadata: %s", data)
	}
	var record nativeS9Diagnostic
	if err := json.Unmarshal(data, &record); err != nil || record.Files["actual-answer.json"] != "readable" {
		t.Fatalf("actual read observation missing: %#v %v", record, err)
	}
	repairPointerCLI(snapshot.State)["plan_sha256"] = strings.Repeat("f", 64)
	if _, found := nativeS9ReturnDiagnosticRecordContext(context.Background(), root, snapshot, key); !found {
		t.Fatal("historical SHA changed the original responsibility's diagnostic")
	}
	repairPointerCLI(snapshot.State)["plan_ref"] = "different-original-plan.json"
	if _, found := nativeS9ReturnDiagnosticRecordContext(context.Background(), root, snapshot, key); found {
		t.Fatal("different original plan reused the earlier diagnostic")
	}
}

func TestRepairDispatchOwnerUsesOriginalResponsibilityNotDigest(t *testing.T) {
	state := attemptDeliverySnapshot().State
	pointer := repairPointerCLI(state)
	row := repairDispatchChoice{AssignmentID: "repair-assignment-foo_bar"}
	first := repairDispatchOwner(state, row)
	pointer["plan_sha256"] = strings.Repeat("a", 64)
	if got := repairDispatchOwner(state, row); got != first {
		t.Fatalf("historical hash changed generated owner: %q != %q", got, first)
	}
	if got := repairDispatchOwner(state, repairDispatchChoice{AssignmentID: "repair-assignment-foo-bar"}); got == first {
		t.Fatal("different original responsibility IDs collided after punctuation normalization")
	}
	pointer["assignment_owners"] = map[string]any{row.AssignmentID: "previously-issued-owner"}
	if got := repairDispatchOwner(state, row); got != "previously-issued-owner" {
		t.Fatalf("already-issued owner was replaced: %q", got)
	}
}

func TestRepairDispatchLongDefaultOwnerThroughOriginalEntry(t *testing.T) {
	root, snapshot, plan, path := repairDispatchFixture(t)
	snapshot.State["schema_version"] = "1.2.0"
	mapFieldCLI(snapshot.State, "configuration")["optimization"] = map[string]any{"packet": true}
	putInitMap(t, filepath.Join(root, ".claude/loop-state.json"), snapshot.State)
	// Simulate the same professional Plan with a long valid Assignment ID.
	longID := "repair-assignment-" + strings.Repeat("long-id-", 30) + "tail"
	plan.Assignments[0].AssignmentID = longID
	planPath := resolveRootPath(root, stringValue(repairPointerCLI(snapshot.State)["plan_ref"]))
	data, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(planPath, data, 0600); err != nil {
		t.Fatal(err)
	}
	rows := []repairDispatchChoice{{AssignmentID: longID, RoleFamily: "backend-builder"}}
	feedback := repairDispatchSave(t, root, path, rows)
	registered := repairDispatchSnapshot(t, root)
	owner := stringMapCLI(repairPointerCLI(registered.State)["assignment_owners"])[longID]
	if owner == "" || !strings.Contains(feedback, owner) {
		t.Fatalf("original dispatch did not register the generated owner: %s", feedback)
	}
	if repairDispatchOwner(registered.State, rows[0]) != owner {
		t.Fatal("original responsibility did not reuse its issued owner")
	}
	for _, task := range objectSliceAny(mapFieldCLI(registered.State, "entities")["tasks"]) {
		p := stringValue(task["path"])
		if p == "" {
			continue
		}
		if _, err := os.Stat(resolveRootPath(root, p)); err != nil {
			t.Fatalf("actual dispatch task path is unusable: %s: %v", p, err)
		}
	}
	// The natural observation sees the same selection after a successful save.
	context := stageRepairDispatchContext(root, policy.Input{Event: "PreToolUse", AgentID: "Main"}, registered)
	if !strings.Contains(context, owner) {
		t.Fatalf("current guidance lost the issued owner: %s", context)
	}
}

func TestS8PacketSchemaAcceptsOriginalFindingPathWithoutSHA(t *testing.T) {
	f := newS8ListenerFixture(t)
	packet := f.packet
	if len(packet.Inputs.EvidenceRefs) < 2 {
		t.Fatal("original Finding evidence is unavailable")
	}
	packet.Inputs.EvidenceRefs[1].Attestation = "finding_source"
	packet.Inputs.EvidenceRefs[1].Path = "docs/reports/finding-source.txt"
	data, err := json.Marshal(packet)
	if err != nil {
		t.Fatal(err)
	}
	if err := schema.NewEmbeddedValidator().ValidateBytes("hypothesis-result-work-packet.schema.json", data); err != nil {
		t.Fatalf("actual original Finding path was made dependent on SHA: %v", err)
	}
}
