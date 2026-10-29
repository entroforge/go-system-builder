package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/packetbody"
	"github.com/entroforge/go-system-builder/internal/policy"
	"github.com/entroforge/go-system-builder/internal/runtime"
)

func TestS9ExternalPacketListenerAndObserverPreserveExecutionAndDraft(t *testing.T) {
	root, _, plan, input, _ := nativeS9WorkerFixture(t)
	input.Event = "SubagentStop"
	var out, diagnostic bytes.Buffer
	if handled, code := nativeS9Hook(root, input, &out, &diagnostic); !handled || code != 0 {
		t.Fatalf("receive original native artifacts: %t %d %s %s", handled, code, &out, &diagnostic)
	}
	snapshot := repairDispatchSnapshot(t, root)
	data, err := json.Marshal(snapshot.State)
	if err != nil {
		t.Fatal(err)
	}
	var inline map[string]any
	if err := json.Unmarshal(data, &inline); err != nil {
		t.Fatal(err)
	}
	ctx := runtime.WithPacketBodyRoot(context.Background(), root)
	entities := mapFieldCLI(inline, "entities")
	rows := entities["work_packets"].([]any)
	externalCount := 0
	for i, raw := range rows {
		row := raw.(map[string]any)
		if row["template_id"] != "s9-plan-report-v1" && row["template_id"] != "s9-repair-result-v1" {
			continue
		}
		// Construct the legacy comparison snapshot explicitly in this test. Normal
		// snapshots and production observers keep the external index representation.
		_, external, err := packetbody.ReferenceFromIndex(row)
		if err != nil {
			t.Fatal(err)
		}
		if !external {
			index, publishErr := packetbody.Publish(context.Background(), root, row)
			if publishErr != nil {
				t.Fatal(publishErr)
			}
			row = index
			// Exercise external projections even if automatic publication is disabled.
			snapshot.State["entities"].(map[string]any)["work_packets"].([]any)[i] = index
		}
		body, err := packetbody.Read(ctx, row)
		if err != nil {
			t.Fatal(err)
		}
		rows[i] = body
		externalCount++
	}
	if externalCount < 2 {
		t.Fatalf("fixture did not retain both original S9 packet kinds: %d", externalCount)
	}
	for _, result := range []bool{false, true} {
		want, err := s9ListenerQuestionnairesContext(ctx, root, inline, plan.Binding.OwnerAgentID, result)
		if err != nil || len(want) == 0 {
			t.Fatalf("inline listener result=%v: %v %v", result, want, err)
		}
		got, err := s9ListenerQuestionnairesContext(ctx, root, snapshot.State, plan.Binding.OwnerAgentID, result)
		if err != nil {
			t.Fatal(err)
		}
		for i := range want {
			want[i].draft = nil
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("external state-only listener result=%v changed the recorded source/path", result)
		}
		for _, packet := range got {
			if packet.draft != nil || result && packet.originalRoot == "" {
				t.Fatalf("state-only listener loaded a body or lost recorded source: %+v", packet)
			}
		}
	}
	request := policy.Input{Event: "PreToolUse", AgentID: plan.Binding.OwnerAgentID, CWD: root}
	want := s9ObservableQuestionnaires(root, inline, request)
	got := s9ObservableQuestionnaires(root, snapshot.State, request)
	if len(want) == 0 {
		t.Fatal("fixture did not expose an original worker questionnaire")
	}
	for i := range got {
		if got[i].s9Packet != nil {
			draft, err := s9ListenerQuestionnaireDraft(ctx, root, snapshot.State, *got[i].s9Packet)
			if err != nil || !reflect.DeepEqual(draft, want[i].draft) {
				t.Fatalf("budgeted original draft differs from inline: %v", err)
			}
			if got[i].draft != nil {
				t.Fatal("observer enumeration eagerly loaded a body")
			}
			want[i].draft = nil
			want[i].s9Packet.draft = nil
		}
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatal("external packet changed recorded observer coordinates")
	}

}

func TestS9ExternalListenerUsesFrozenLocatorWithoutBodyOrLiveProof(t *testing.T) {
	f := newS9ListenerFixture(t, true)
	index, err := packetbody.Publish(t.Context(), f.root, f.row)
	if err != nil {
		t.Fatal(err)
	}
	mapFieldCLI(f.state, "entities")["work_packets"] = []any{index}
	ref, _, err := packetbody.ReferenceFromIndex(index)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(resolveRootPath(f.root, ref.Path)); err != nil {
		t.Fatal(err)
	}
	reads := 0
	ctx := packetbody.WithResolver(t.Context(), func(context.Context, packetbody.Reference) ([]byte, error) {
		reads++
		return nil, errors.New("progress must not read the body")
	})
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	packets, err := s9ListenerQuestionnairesContext(canceled, f.root, f.state, f.packet.binding.OwnerAgentID, true)
	if err != nil || len(packets) != 1 || packets[0].originalRoot != f.worker || packets[0].draft != nil || reads != 0 {
		t.Fatalf("indexed progress required body/proof: packets=%+v reads=%d err=%v", packets, reads, err)
	}
	message := s9QuestionnaireContext(canceled, f.root, policy.Input{Event: "PreToolUse", AgentID: "Main"}, nil, runtime.Snapshot{State: f.state}, "Main", true)
	if !strings.Contains(message, resolveRootPath(f.worker, f.packet.answer)) || !strings.Contains(message, "Filesystem preparation was not refreshed within this request") || strings.Contains(message, "Original acceptance source is unavailable") || strings.Contains(message, "not prepared") {
		t.Fatalf("cancellation erased known responsibility: %s", message)
	}
	execution := s9ObservedExecution(f.state, packets[0])
	changedRoot := t.TempDir()
	execution["worktree_path"], execution["execution_generation"] = changedRoot, 2
	packets, err = s9ListenerQuestionnairesContext(canceled, f.root, f.state, f.packet.binding.OwnerAgentID, true)
	if err != nil || len(packets) != 0 {
		t.Fatalf("changed execution retained old active questionnaire: %v %v", packets, err)
	}
	paths := s9ListenerKnownPaths(f.root, f.state, f.packet.binding.OwnerAgentID, true)
	if !strings.Contains(paths, resolveRootPath(f.worker, f.packet.answer)) || strings.Contains(paths, changedRoot) {
		t.Fatalf("mutable registry replaced original frozen locator: %s", paths)
	}
	if reads != 0 {
		t.Fatal("state-only projection read or reconstructed a packet body")
	}
}

func TestS9ExternalObserverReportsUnknownDraftWhenBudgetIsCanceled(t *testing.T) {
	f := newS9ListenerFixture(t, true)
	index, err := packetbody.Publish(t.Context(), f.root, f.row)
	if err != nil {
		t.Fatal(err)
	}
	mapFieldCLI(f.state, "entities")["work_packets"] = []any{index}
	reads := 0
	ctx := packetbody.WithResolver(t.Context(), func(context.Context, packetbody.Reference) ([]byte, error) {
		reads++
		return nil, errors.New("canceled draft observation must not start IO")
	})
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	questions := s9ObservableQuestionnaires(f.root, f.state, policy.Input{Event: "PostToolUse", AgentID: f.packet.binding.OwnerAgentID, CWD: f.worker, Context: canceled})
	if len(questions) != 1 || questions[0].path != resolveRootPath(f.worker, f.packet.answer) || questions[0].draft != nil || reads != 0 {
		t.Fatalf("canceled enumeration lost recorded path or loaded a body: %+v reads=%d", questions, reads)
	}
	if _, err := s9ListenerQuestionnaireDraft(canceled, f.root, f.state, *questions[0].s9Packet); !errors.Is(err, context.Canceled) {
		t.Fatalf("budgeted comparison did not preserve unknown/canceled result: %v", err)
	}
	if reads != 0 {
		t.Fatal("canceled comparison started body IO")
	}

}
