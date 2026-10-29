package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/entroforge/go-system-builder/internal/hook"
	"github.com/entroforge/go-system-builder/internal/hookctx"
	"github.com/entroforge/go-system-builder/internal/policy"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

func TestProgressPacketS9SkipsLegacyCoverageWithoutCache(t *testing.T) {
	f := newS9ListenerFixture(t, true)
	f.state["bound_req"] = map[string]any{"id": "REQ-s9-progress"}
	for _, phase := range []string{"repair_readback", "planning", "reproducing", "fixing", "targeted_reverification", "ready_for_full_review"} {
		mapFieldCLI(f.state, "lifecycle")["phase"] = phase
		facts, execution, err := projectWorkProgress(f.root, policy.Input{AgentID: "native-platform-id", SessionID: "s9-progress", CWD: f.worker}, runtime.Snapshot{State: f.state})
		if err != nil || facts.Known || facts.Owner != "" || facts.Binding != "" || facts.Complete || len(facts.Sources) != 0 || execution != nil {
			t.Fatalf("%s entered legacy actor/packet coverage or inferred completion: %+v %+v %v", phase, facts, execution, err)
		}
	}
	mapFieldCLI(f.state, "lifecycle")["phase"] = "fixing"
	f.write(t)
	before := repairDispatchRuntimeBytes(t, f.root)
	for _, owner := range []string{"Main", f.packet.binding.OwnerAgentID, "native-platform-id"} {
		for _, tool := range []string{"Read", "Bash", "Write"} {
			request := policy.Input{Event: "PostToolUse", ToolName: tool, AgentID: owner, SessionID: "s9-progress", CWD: f.root, ToolInput: map[string]any{"file_path": filepath.Join(f.root, ".claude/loop-state.json")}}
			var diagnostic bytes.Buffer
			if notice, _, _ := observeWorkProgress(f.root, request, &diagnostic); notice != nil || diagnostic.Len() != 0 {
				t.Fatalf("%s %s emitted legacy S9 coverage noise: notice=%+v stderr=%s", owner, tool, notice, diagnostic.String())
			}
		}
	}
	if _, err := os.Stat(filepath.Join(f.root, ".claude/hook-notices/work-progress")); !os.IsNotExist(err) {
		t.Fatalf("unknown S9 telemetry created a progress cache: %v", err)
	}
	repairDispatchAssertUnchanged(t, f.root, before)
}

func TestProgressProjectionIgnoresRevisionClockAndControlBytes(t *testing.T) {
	root := initFormatRoot(t)
	state, err := inactiveRuntimeState(root, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	state["lifecycle"].(map[string]any)["state"] = "planning"
	state["bound_req"] = map[string]any{"id": "REQ-progress", "path": "docs/requirements/REQ-progress.md", "version": "v1.0.0", "sha256": workpacket.Digest([]byte("requirement"))}
	milestone := state["milestone"].(map[string]any)
	milestone["lifecycle_state"] = "planning"
	milestone["read"] = []any{".claude/loop-state.json"}
	putInitMap(t, filepath.Join(root, ".claude/loop-state.json"), state)
	input := policy.Input{CWD: root, SessionID: "session"}
	f, _, err := projectWorkProgress(root, input, runtime.Snapshot{State: state})
	if err != nil || !f.Known {
		t.Fatal(f, err)
	}
	state["revision"] = 99
	state["updated_at"] = "new clock"
	milestone = state["milestone"].(map[string]any)
	milestone["source_revision"] = 99
	milestone["updated_at"] = "new clock"
	milestone["event"] = "another notification"
	putInitMap(t, filepath.Join(root, ".claude/loop-state.json"), state)
	other, _, err := projectWorkProgress(root, input, runtime.Snapshot{State: state, Revision: 99})
	if err != nil || f.Binding != other.Binding || f.Semantic != other.Semantic || f.Gap != other.Gap {
		t.Fatal("observer churn became professional progress", f, other, err)
	}
	if len(other.Sources) != 0 {
		t.Fatal("Runtime bytes counted as required-source progress", other.Sources)
	}
}

func TestProgressClosedStatusAndActualReadResponse(t *testing.T) {
	root := t.TempDir()
	input := policy.Input{CWD: root, ToolInput: map[string]any{}}
	for _, command := range []string{".claude/bin/loop-harness status --root '" + root + "'", ".claude/bin/loop-harness next --json", "git status --short"} {
		input.ToolInput["command"] = command
		if !progressStatusCommand(root, input, nil) {
			t.Fatal("declared observation lost", command)
		}
	}
	for _, command := range []string{".claude/bin/loop-harness status; touch bad", "git status && rm data", ".claude/bin/loop-harness req bind", ".claude/bin/loop-harness status --root /other", "echo hi", "go test ./..."} {
		input.ToolInput["command"] = command
		if progressStatusCommand(root, input, nil) {
			t.Fatal("undeclared execution classified as status", command)
		}
	}
	path := filepath.Join(root, "required.md")
	body := []byte("one\ntwo\n")
	os.WriteFile(path, body, 0600)
	sources := map[string]string{path: workpacket.Digest(body)}
	input.ToolInput = map[string]any{"file_path": path, "offset": 2, "limit": 1}
	input.ToolResponse = map[string]any{"file": map[string]any{"filePath": path, "startLine": float64(2), "numLines": float64(1), "content": "two\n"}}
	if read := actualProgressRead(root, input, sources); read == nil || read.Range != "2:1" {
		t.Fatal("actual necessary range not observed", read)
	}
	input.ToolResponse["file"].(map[string]any)["content"] = "unproven"
	if actualProgressRead(root, input, sources) != nil {
		t.Fatal("unverified returned bytes were progress")
	}
	input.ToolResponse = nil
	if actualProgressRead(root, input, sources) != nil {
		t.Fatal("missing actual trace counted as success")
	}
}

func TestProgressProfessionalMetadataAndOwnedCodeBounds(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "plan.json")
	os.WriteFile(path, []byte("{\"message_id\":\"one\",\"updated_at\":\"old\",\"scope\":\"real\"}"), 0600)
	before, err := progressProfessionalArtifact(root, "plan.json")
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(path, []byte("{\"message_id\":\"two\",\"updated_at\":\"new\",\"scope\":\"real\"}"), 0600)
	after, err := progressProfessionalArtifact(root, "plan.json")
	if err != nil || before != after {
		t.Fatal("message metadata alone counted as progress")
	}
	os.Mkdir(filepath.Join(root, "src"), 0700)
	os.WriteFile(filepath.Join(root, "src/a.go"), []byte("real code"), 0600)
	budget := int64(4 << 20)
	first, err := progressOwnedCode(root, []string{"src/**"}, &budget)
	if err != nil || len(first) != 1 {
		t.Fatal(first, err)
	}
	os.WriteFile(filepath.Join(root, "src/a.go"), []byte("changed code"), 0600)
	budget = 4 << 20
	second, err := progressOwnedCode(root, []string{"src/**"}, &budget)
	a, _ := json.Marshal(first)
	b, _ := json.Marshal(second)
	if err != nil || string(a) == string(b) {
		t.Fatal("owned code edit ignored")
	}
	budget = 1
	if _, err := progressOwnedCode(root, []string{"src/**"}, &budget); err == nil {
		t.Fatal("unbounded source observation")
	}
}

func TestProgressProjectionAcceptsEighteenFrozenS5SourcesAndDeduplicates(t *testing.T) {
	root := t.TempDir()
	refs := make([]workpacket.Reference, 18)
	var total int64
	for i := range refs {
		path := fmt.Sprintf("docs/source-%02d.md", i+1)
		body := []byte(fmt.Sprintf("frozen source %02d\n", i+1))
		if err := os.MkdirAll(filepath.Dir(filepath.Join(root, path)), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, path), body, 0600); err != nil {
			t.Fatal(err)
		}
		refs[i] = workpacket.Reference{ID: fmt.Sprintf("source-%02d", i+1), Path: path, Version: "v1.0.0", SHA256: workpacket.Digest(body)}
		total += int64(len(body))
	}
	packet, err := workpacket.BuildS5(workpacket.Binding{
		RuntimeID: "runtime-progress", Generation: 1, StageEntry: 1, Owner: "reviewer-spec",
		AssignmentID: "assignment-spec", AssignmentSHA: workpacket.Digest([]byte("assignment")),
		Responsibility: "DV-SPEC-CONSISTENCY", Attempt: 1,
	}, refs)
	if err != nil {
		t.Fatal(err)
	}
	// Repeated exact paths do not consume the projection budget twice.
	inputs := append(append([]workpacket.Reference(nil), packet.Inputs...), packet.Inputs[0])
	sources := map[string]string{}
	remaining := progressSourceProjectionByteLimit
	if err := projectProgressSourceInputs(sources, &remaining, root, inputs); err != nil {
		t.Fatal(err)
	}
	if len(sources) != 18 || remaining != progressSourceProjectionByteLimit-total {
		t.Fatalf("projected %d exact sources with %d bytes remaining", len(sources), remaining)
	}
	for _, ref := range packet.Inputs {
		if got := sources[filepath.Join(root, ref.Path)]; got != ref.SHA256 {
			t.Fatalf("source %s lost its frozen SHA: %s", ref.Path, got)
		}
	}

	// A legitimate-looking but changed source remains an incomplete projection.
	if err := os.WriteFile(filepath.Join(root, packet.Inputs[0].Path), []byte("changed after issue\n"), 0600); err != nil {
		t.Fatal(err)
	}
	freshSources := map[string]string{}
	freshBudget := progressSourceProjectionByteLimit
	if err := projectProgressSourceInputs(freshSources, &freshBudget, root, packet.Inputs[:1]); err == nil || err.Error() != "necessary source differs from issued binding" {
		t.Fatal("changed source did not fail its frozen SHA check", err)
	}
}

func TestProgressProjectionReportsAggregateByteBudget(t *testing.T) {
	root := t.TempDir()
	path := "docs/oversized.md"
	if err := os.MkdirAll(filepath.Dir(filepath.Join(root, path)), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, path), make([]byte, progressSourceProjectionByteLimit+1), 0600); err != nil {
		t.Fatal(err)
	}
	remaining := progressSourceProjectionByteLimit
	var sources = map[string]string{}
	_, err := addProgressSource(sources, &remaining, root, path)
	if err == nil || !strings.Contains(err.Error(), "4194304-byte aggregate projection budget") {
		t.Fatalf("over-budget projection was not reported clearly: %v", err)
	}
	if len(sources) != 0 || remaining != progressSourceProjectionByteLimit {
		t.Fatal("over-budget source was partially projected", sources, remaining)
	}
}

func TestProgressWaitsOnSharedResponsibilityFacts(t *testing.T) {
	for _, tc := range []struct {
		name, state, activity, blocker, shutdown, report string
		packetReceived                                   bool
		wantStatus                                       string
		wantComplete, wantWaiting, wantSuppressed        bool
	}{
		{name: "accepted current packet awaits Main", state: "active", activity: "working", packetReceived: true, wantStatus: "done", wantComplete: true, wantWaiting: true, wantSuppressed: true},
		{name: "bare done label", state: "done", activity: "done", wantStatus: "active"},
		{name: "report awaiting consumer", state: "active", activity: "working", report: "completion.json", wantStatus: "active", wantWaiting: true, wantSuppressed: true},
		{name: "approved cancellation", state: "cancelled", activity: "stopped", shutdown: "shutdown.json", wantStatus: "cancelled", wantWaiting: true, wantSuppressed: true},
		{name: "bare cancellation label", state: "cancelled", activity: "stopped", wantStatus: "active"},
		{name: "original unresolved blocker", state: "blocked", activity: "blocked", blocker: "blocker.json", wantStatus: "blocked", wantWaiting: true, wantSuppressed: true},
		{name: "bare blocker label", state: "blocked", activity: "blocked", wantStatus: "active"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			agent := &policy.AgentContext{
				ID: "reviewer-spec", AssignmentID: "assignment-spec", State: tc.state,
				Activity: tc.activity, WorkBlockedRef: tc.blocker,
				ShutdownApprovedRef: tc.shutdown, CompletionReportedRef: tc.report,
			}
			assignment := &hookctx.AssignmentContext{AssignmentID: agent.AssignmentID, OwnerAgentID: agent.ID}
			facts := hook.ProjectResponsibilityWithProofs(agent, assignment, tc.packetReceived, hook.ResponsibilityProofs{})
			if string(facts.Status) != tc.wantStatus {
				t.Fatalf("shared responsibility status=%q want %q", facts.Status, tc.wantStatus)
			}
			progress := hook.ProgressFacts{}
			suppressed := applyResponsibilityProgress(&progress, facts)
			if progress.Complete != tc.wantComplete || progress.Waiting != tc.wantWaiting || suppressed != tc.wantSuppressed {
				t.Fatalf("progress projection complete=%t waiting=%t suppressed=%t; want %t/%t/%t", progress.Complete, progress.Waiting, suppressed, tc.wantComplete, tc.wantWaiting, tc.wantSuppressed)
			}
		})
	}
}
