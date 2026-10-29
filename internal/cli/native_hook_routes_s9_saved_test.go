package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/entroforge/go-system-builder/internal/metrics"
	"github.com/entroforge/go-system-builder/internal/policy"
	"github.com/entroforge/go-system-builder/internal/submission"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

func TestNativeHookRoutesS9MixedAvailableKeepsExactFrozenRejection(t *testing.T) {
	prepared := time.Now()
	root, worker, plan, _, _ := nativeS9WorkerFixture(t)
	ctx := context.Background()
	service, err := submission.New(root)
	if err != nil {
		t.Fatal(err)
	}
	var diagnostic bytes.Buffer
	if err := importNativeS9PlanReport(ctx, root, worker, plan, service, &diagnostic); err != nil {
		t.Fatal(err)
	}
	if receipt, err := service.SaveRepairPlanReportFrom(ctx, plan.Binding.OwnerAgentID, filepath.Join(root, plan.AnswerPath), root, nil); err != nil || !receipt.Accepted {
		t.Fatalf("original Plan reception: %+v %v", receipt, err)
	}
	if err := recordNativeS9Source(ctx, root, worker, repairLeadGit(t, root, "rev-parse", "HEAD"), plan); err != nil {
		t.Fatal(err)
	}
	// Controlled mechanism input, not a claim of a real bootstrap session:
	// select the frozen channel before original issuance freezes its definition.
	// The real Git source, owner, Plan and registry association are retained;
	// no issued packet or execution digest is patched by this fixture.
	snapshot := repairDispatchSnapshot(t, root)
	registry, err := workspace.Decode(snapshot.State)
	if err != nil || registry == nil {
		t.Fatalf("current source registry: %v", err)
	}
	execution := registry.Executions[plan.Binding.ManifestAssignmentID]
	execution.Purpose = ""
	installed, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	// Install real bootstrap bytes and record their actual digest. The test
	// executable is only an installation input; no child session is started.
	if err := registry.Bootstrap(execution, installed); err != nil {
		t.Fatalf("controlled frozen bootstrap: %v", err)
	}
	execution.BootstrapSHA256, err = workspace.BootstrapDigest(execution)
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.ValidateBootstrap(execution); err != nil {
		t.Fatal(err)
	}
	registry.Executions[plan.Binding.ManifestAssignmentID] = execution
	snapshot.State["workspace"] = workspace.Encode(registry)
	putInitMap(t, service.StatePath, snapshot.State)
	packets, err := service.IssueRepairResults(ctx, plan.Binding.OwnerAgentID)
	if err != nil || len(packets) != 1 {
		t.Fatalf("original frozen-channel issuance: %d %v", len(packets), err)
	}
	packet := packets[0]
	if _, selected, err := submission.RepairResultRegisteredSource(root, repairDispatchSnapshot(t, root).State, packet); err != nil || selected.Purpose != "" {
		t.Fatalf("exact frozen source not selected: %+v %v", selected, err)
	}
	if !nativeS9Available(root, repairDispatchSnapshot(t, root).State) {
		t.Fatal("mixed fixture lost native availability")
	}
	path := filepath.Join(worker, packet.AnswerPath)
	if err := os.WriteFile(path, []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Logf("controlled mixed fixture preparation wall=%s", time.Since(prepared))
	before := repairDispatchRuntimeBytes(t, root)
	in := policy.Input{Event: "PostToolUse", AgentID: "opaque-frozen-save", CWD: worker, ToolName: "Write", ToolInput: map[string]any{"file_path": path}}
	var out bytes.Buffer
	diagnostic.Reset()
	timed, spans := metrics.WithCallTimings(context.Background())
	in.Context = timed
	start := time.Now()
	handled, code := nativeHookRoutes(root, in, &out, &diagnostic)
	t.Logf("Hook wall=%s output_bytes=%d diagnostic_bytes=%d", time.Since(start), out.Len(), diagnostic.Len())
	t.Logf("existing spans=%v (overlapping, not summed)", spans.Snapshot())
	if !handled || code != 0 || !strings.Contains(out.String(), "not_accepted") || !strings.Contains(diagnostic.String(), "S9 RepairResult not accepted") {
		t.Fatalf("broad native quiet swallowed frozen professional rejection: handled=%t code=%d out=%s diagnostic=%s", handled, code, &out, &diagnostic)
	}
	repairDispatchAssertUnchanged(t, root, before)
}

func TestNativeHookRoutesS9ExactNativeOrdinaryQuietBeforeBroadFallback(t *testing.T) {
	prepared := time.Now()
	root, worker, _, packet, state, journal, _, _ := nativeS9OriginalCandidateBoundary(t)
	// Return to the original, valid issued-source boundary, before candidate
	// acceptance. No packet, source definition or generation is manufactured.
	if err := os.WriteFile(filepath.Join(root, ".claude/loop-state.json"), state, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".claude/loop-events.jsonl"), journal, 0600); err != nil {
		t.Fatal(err)
	}
	if !nativeS9Available(root, repairDispatchSnapshot(t, root).State) {
		t.Fatal("fixture must exercise broad native availability")
	}
	t.Logf("fixture preparation wall=%s", time.Since(prepared))
	path := filepath.Join(worker, packet.AnswerPath)
	in := policy.Input{Event: "PostToolUse", AgentID: "opaque-platform-save", CWD: worker, ToolName: "Write", ToolInput: map[string]any{"file_path": path}}
	for _, mode := range []string{"bad-json", "missing-answer-response-fallback", "unissued-response-fallback"} {
		t.Run(mode, func(t *testing.T) {
			request := in
			timed, spans := metrics.WithCallTimings(context.Background())
			request.Context = timed
			switch mode {
			case "bad-json":
				if err := os.WriteFile(path, []byte("{}"), 0600); err != nil {
					t.Fatal(err)
				}
			case "missing-answer-response-fallback":
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				request.ToolInput = nil
				request.ToolResponse = map[string]any{"file_path": path}
			case "unissued-response-fallback":
				request.ToolInput = nil
				request.ToolResponse = map[string]any{"file_path": filepath.Join(filepath.Dir(path), "unissued", "answer.json")}
			}
			before := repairDispatchRuntimeBytes(t, root)
			var out, diagnostic bytes.Buffer
			start := time.Now()
			handled, code := nativeHookRoutes(root, request, &out, &diagnostic)
			t.Logf("Hook wall=%s output_bytes=%d diagnostic_bytes=%d existing_spans=%v (overlapping, not summed)", time.Since(start), out.Len(), diagnostic.Len(), spans.Snapshot())
			if !handled || code != 0 {
				t.Fatalf("handled=%t code=%d out=%s diagnostic=%s", handled, code, &out, &diagnostic)
			}
			if mode == "unissued-response-fallback" {
				if !strings.Contains(out.String(), "not_accepted") || !strings.Contains(diagnostic.String(), "S9 RepairResult not accepted") {
					t.Fatalf("broad native quiet swallowed exact-family source error: %s %s", &out, &diagnostic)
				}
			} else if out.Len() != 0 || diagnostic.Len() != 0 {
				t.Fatalf("native ordinary save read private answer or emitted receipt: %s %s", &out, &diagnostic)
			}
			repairDispatchAssertUnchanged(t, root, before)
		})
	}
}
