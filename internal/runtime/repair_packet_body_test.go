package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/packetbody"
)

func externalizeS9FixtureRows(t *testing.T, root string, state map[string]any) map[string][]byte {
	t.Helper()
	bodies := map[string][]byte{}
	entities := state["entities"].(map[string]any)
	rows := entities["work_packets"].([]any)
	for i, raw := range rows {
		row := raw.(map[string]any)
		if row["template_id"] != repairPlanReportPacketTemplateID && row["template_id"] != repairResultPacketTemplateID {
			continue
		}
		index, err := packetbody.Publish(context.Background(), root, row)
		if err != nil {
			t.Fatal(err)
		}
		ref, _, err := packetbody.ReferenceFromIndex(index)
		if err != nil {
			t.Fatal(err)
		}
		body, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(ref.Path)))
		if err != nil {
			t.Fatal(err)
		}
		bodies[ref.Path] = body
		rows[i] = index
	}
	return bodies
}

func TestS9ExternalPlanBodyKeepsOriginalQuestionReceiptAndSources(t *testing.T) {
	f := newRepairPlanReportRuntimeFixture(t)
	accepted := acceptRepairPlanReportFixture(t, &f)
	externalizeS9FixtureRows(t, f.root, f.state)
	ctx := WithPacketBodyRoot(context.Background(), f.root)
	packet, found, err := FindRepairPlanReportPacketContext(ctx, f.state, f.packet.ID)
	if err != nil || !found || !reflect.DeepEqual(packet, f.packet) {
		t.Fatalf("original packet changed: %v %v", found, err)
	}
	receipt, found, err := VerifiedRepairPlanReportReceipt(f.root, f.state, f.packet.ID, f.packet.Binding.OwnerAgentID)
	if err != nil || !found || !reflect.DeepEqual(receipt, accepted) {
		t.Fatalf("historical receipt changed: %v %v", found, err)
	}
	if err := VerifyRepairPlanReportPacketIssueContext(ctx, f.root, f.state, packet); err != nil {
		t.Fatal(err)
	}
	// External storage never replaces the original source/approval closure.
	source := filepath.Join(f.root, filepath.FromSlash(packet.Binding.PlanRef))
	if err := os.WriteFile(source, []byte(`{"changed":"original plan source"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := VerifyRepairPlanReportPacketIssueContext(ctx, f.root, f.state, packet); err == nil {
		t.Fatal("external body granted source authority after drift")
	}
}

func TestS9ExternalSelectorsReadOnlySelectedBodies(t *testing.T) {
	for _, kind := range []string{"plan", "result"} {
		t.Run(kind, func(t *testing.T) {
			f := newRepairResultRuntimeFixture(t)
			bodies := externalizeS9FixtureRows(t, f.plan.root, f.plan.state)
			rows := f.plan.state["entities"].(map[string]any)["work_packets"].([]any)
			template := repairResultPacketTemplateID
			if kind == "plan" {
				template = repairPlanReportPacketTemplateID
			}
			var selected map[string]any
			for _, raw := range rows {
				row := raw.(map[string]any)
				if row["template_id"] == template {
					selected = row
					break
				}
			}
			// Structurally valid historical indices need no current body read. Their
			// exact body mismatch remains an error when that history is explicitly read.
			historical := s8CheckpointCloneMap(t, selected)
			historical["packet_id"] = "packet-" + strings.Repeat("e", 64)
			binding := historical["binding"].(map[string]any)
			if kind == "result" {
				binding = binding["sources"].(map[string]any)
			}
			binding["baseline_generation"] = 99
			f.plan.state["entities"].(map[string]any)["work_packets"] = append(rows, historical)
			reads := 0
			ctx := packetbody.WithResolver(WithPacketBodyRoot(context.Background(), f.plan.root), func(_ context.Context, ref packetbody.Reference) ([]byte, error) {
				reads++
				data, ok := bodies[ref.Path]
				if !ok {
					return nil, os.ErrNotExist
				}
				return data, nil
			})
			if kind == "plan" {
				packets, err := CurrentRepairPlanReportPacketsContext(ctx, f.plan.root, f.plan.state, f.plan.packet.Binding.OwnerAgentID)
				if err != nil || len(packets) != 1 {
					t.Fatalf("current plan: %v %v", packets, err)
				}
			} else {
				packets, err := CurrentRepairResultPacketsContext(ctx, f.plan.root, f.plan.state, f.packet.Binding.Sources.OwnerAgentID)
				if err != nil || len(packets) != 1 {
					t.Fatalf("current result: %v %v", packets, err)
				}
			}
			if reads != 1 {
				t.Fatalf("selected lookup read %d bodies, want 1", reads)
			}
			if kind == "plan" {
				if _, _, err := FindRepairPlanReportPacketContext(ctx, f.plan.state, historical["packet_id"].(string)); err == nil {
					t.Fatal("historical identity mismatch hidden")
				}
			} else {
				if _, _, err := FindRepairResultPacketContext(ctx, f.plan.state, historical["packet_id"].(string)); err == nil {
					t.Fatal("historical identity mismatch hidden")
				}
			}
		})
	}
}

func TestS9ExternalBodyResumeReadsOriginalPredecessorAndSuccessor(t *testing.T) {
	f, successor := issueRepairResultResumeSuccessorFixture(t)
	externalizeS9FixtureRows(t, f.runtime.plan.root, f.runtime.plan.state)
	current, err := CurrentRepairResultPackets(f.runtime.plan.root, f.runtime.plan.state, successor.Binding.Sources.OwnerAgentID)
	if err != nil || len(current) != 1 || !reflect.DeepEqual(current[0], successor) {
		t.Fatalf("Resume successor changed: %v %v", current, err)
	}
	ctx := WithPacketBodyRoot(context.Background(), f.runtime.plan.root)
	original, found, err := FindRepairResultPacketContext(ctx, f.runtime.plan.state, f.runtime.packet.ID)
	if err != nil || !found || !reflect.DeepEqual(original, f.runtime.packet) {
		t.Fatalf("historical predecessor lost: %v %v", found, err)
	}
	if err := VerifyRepairResultPacketIssueContext(ctx, f.runtime.plan.root, f.runtime.plan.state, successor); err != nil {
		t.Fatal(err)
	}
}

func TestS9ExternalCheckpointUsesIndependentBodyResolver(t *testing.T) {
	f := newRepairResultRuntimeFixture(t)
	bodies := externalizeS9FixtureRows(t, f.plan.root, f.plan.state)
	before, _ := json.Marshal(f.plan.state)
	for path := range bodies {
		if err := os.Remove(filepath.Join(f.plan.root, filepath.FromSlash(path))); err != nil {
			t.Fatal(err)
		}
	}
	reads := 0
	ctx := packetbody.WithResolver(context.Background(), func(_ context.Context, ref packetbody.Reference) ([]byte, error) {
		reads++
		data, ok := bodies[ref.Path]
		if !ok {
			return nil, os.ErrNotExist
		}
		return data, nil
	})
	if err := ValidateCheckpointIndicesContext(ctx, f.plan.root, f.plan.state, f.plan.journal); err != nil {
		t.Fatalf("independent original bodies did not restore checkpoint proof: %v", err)
	}
	if reads != 2 {
		t.Fatalf("checkpoint body reads=%d want both S9 bodies once", reads)
	}
	after, _ := json.Marshal(f.plan.state)
	if string(before) != string(after) {
		t.Fatal("checkpoint validation hydrated or mutated live state")
	}
}

func TestS9ExternalBodyRejectsWrongRootDriftFormatsAndCancellation(t *testing.T) {
	for _, change := range []string{"root", "bytes", "format", "unknown-index-field", "cancel"} {
		t.Run(change, func(t *testing.T) {
			f := newRepairResultRuntimeFixture(t)
			externalizeS9FixtureRows(t, f.plan.root, f.plan.state)
			root := f.plan.root
			ctx := context.Background()
			rows := f.plan.state["entities"].(map[string]any)["work_packets"].([]any)
			var index map[string]any
			for _, raw := range rows {
				row := raw.(map[string]any)
				if row["packet_id"] == f.packet.ID {
					index = row
				}
			}
			switch change {
			case "root":
				root = t.TempDir()
			case "bytes":
				ref, _, _ := packetbody.ReferenceFromIndex(index)
				if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(ref.Path)), []byte("changed"), 0o600); err != nil {
					t.Fatal(err)
				}
			case "format":
				index["storage_format"] = "future-format"
			case "unknown-index-field":
				index["future_field"] = true
			case "cancel":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			_, _, err := FindRepairResultPacketContext(WithPacketBodyRoot(ctx, root), f.plan.state, f.packet.ID)
			if err == nil {
				t.Fatalf("unsafe %s accepted", change)
			}
			if change == "cancel" && !errors.Is(err, context.Canceled) {
				t.Fatalf("cancellation lost: %v", err)
			}
		})
	}
}

func TestS9ExecutionLocatorCannotReplaceFrozenBodySource(t *testing.T) {
	f := newRepairResultRuntimeFixture(t)
	externalizeS9FixtureRows(t, f.plan.root, f.plan.state)
	var index map[string]any
	for _, raw := range f.plan.state["entities"].(map[string]any)["work_packets"].([]any) {
		row := raw.(map[string]any)
		if row["packet_id"] == f.packet.ID {
			index = row
		}
	}
	locator := index["execution_locator"].(map[string]any)
	var original map[string]any
	if err := json.Unmarshal(f.packet.Inputs.Execution, &original); err != nil {
		t.Fatal(err)
	}
	if locator["assignment_id"] != original["assignment_id"] || locator["worktree_path"] != original["worktree_path"] {
		t.Fatal("index did not retain the exact original source locator")
	}
	locator["worktree_path"] = t.TempDir()
	ctx := WithPacketBodyRoot(t.Context(), f.plan.root)
	if _, _, err := FindRepairResultPacketContext(ctx, f.plan.state, f.packet.ID); err == nil {
		t.Fatal("changed lightweight source locator authorized a different original body")
	}
}
