package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/cli"
	reviewpkg "github.com/entroforge/go-system-builder/internal/review"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/semantic"
)

func TestS10QuestionnaireHookUsesPreparedManifestAndMainProducer(t *testing.T) {
	for _, tool := range []string{"Write", "Bash", "mcp__sandbox__execute"} {
		t.Run(tool, func(t *testing.T) { testS10QuestionnaireSave(t, tool) })
	}
}

func TestS10PreparedQuestionnaireBindsMachineFieldsWithoutAuthorReentry(t *testing.T) {
	for _, machineInput := range []string{"omitted", "stale"} {
		t.Run(machineInput, func(t *testing.T) {
			// The original stage-entry action prepares this answer from existing
			// upstream facts. No S10 metadata or envelope is assembled by the author.
			root, path := s10QuestionnaireHookRoot(t)
			prepared := readS10QuestionnaireState(t, path)
			fillS10QuestionnaireAnswer(t, path, "acceptance", "pass")
			answer := readS10QuestionnaireState(t, path)
			fields := []string{"schema_version", "manifest_type", "runtime_id", "baseline_generation", "review_round", "metrics"}
			for _, field := range fields {
				delete(answer, field)
			}
			if machineInput == "stale" {
				answer["schema_version"] = "old"
				answer["manifest_type"] = "release_audit"
				answer["runtime_id"] = "another-runtime"
				answer["baseline_generation"] = 99
				answer["review_round"] = 99
				answer["metrics"] = map[string]any{"unknown_count": 99}
			}
			// Selecting real professional material does not require registering
			// another ID or copying its source association into an evidence table.
			const observation = "docs/reports/synthetic-s10-observation.txt"
			writeS10QuestionnaireFile(t, root, observation, []byte("Synthetic fixture observation only; no product validation is claimed.\n"))
			for _, raw := range answer["coverage_inventory"].([]any) {
				raw.(map[string]any)["evidence_refs"] = []string{observation}
			}
			data, err := json.MarshalIndent(answer, "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
				t.Fatal(err)
			}
			out := callS10QuestionnaireHook(t, root, map[string]any{
				"hook_event_name": "PostToolUse", "cwd": root, "tool_name": "Write",
				"tool_input": map[string]any{"file_path": path}, "tool_response": map[string]any{},
			})
			state := readS10QuestionnaireState(t, filepath.Join(root, ".claude", "loop-state.json"))
			if state["lifecycle"].(map[string]any)["state"] != "release_audit" || !hasS10QuestionnaireEvidence(root, state, "acceptance", "pass") {
				t.Fatalf("professional answer did not reach the original TR-015: %s", out)
			}
			received := readS10QuestionnaireState(t, path)
			for _, field := range fields[:5] {
				if received[field] != prepared[field] {
					t.Fatalf("system did not bind current %s: got %#v, want %#v", field, received[field], prepared[field])
				}
			}
			if received["metrics"].(map[string]any)["unknown_count"] != float64(0) {
				t.Fatal("system did not derive metrics from actual reported rows")
			}
			for _, raw := range state["evidence"].([]any) {
				if raw.(map[string]any)["path"] == observation {
					t.Fatal("professional file reference was registered as a new qualification")
				}
			}
			for _, next := range []string{strings.TrimSuffix(path, ".json") + ".md", filepath.Join(filepath.Dir(path), "release_audit-manifest.json")} {
				if _, err := os.Stat(next); err != nil {
					t.Fatalf("original outlet omitted its report/next questionnaire %s: %v", next, err)
				}
			}
		})
	}
}

func testS10QuestionnaireSave(t *testing.T, tool string) {
	t.Helper()
	root, manifestPath := s10QuestionnaireHookRoot(t)
	request := map[string]any{
		"hook_event_name": "PreToolUse", "cwd": root, "tool_name": "Write",
		"tool_input": map[string]any{"file_path": manifestPath},
	}
	statePath := filepath.Join(root, ".claude", "loop-state.json")
	beforePre, _ := os.ReadFile(statePath)
	beforeDraft, _ := os.ReadFile(manifestPath)
	out := callS10QuestionnaireHook(t, root, request)
	if !strings.Contains(out, manifestPath) {
		t.Fatalf("PreToolUse did not give the exact absolute manifest path: %s", out)
	}

	afterPre, _ := os.ReadFile(statePath)
	afterDraft, _ := os.ReadFile(manifestPath)
	if !bytes.Equal(beforePre, afterPre) || !bytes.Equal(beforeDraft, afterDraft) {
		t.Fatal("ordinary S10 Pre mutated Runtime or its existing draft")
	}

	request["hook_event_name"] = "PostToolUse"
	request["tool_name"] = tool
	request["tool_response"] = map[string]any{}
	if tool != "Write" {
		request["tool_input"] = map[string]any{"command": "script saved the prepared manifest; command text is not parsed"}
	}
	// Draft saves do not imply a negative route or an accepted professional conclusion.
	out = callS10QuestionnaireHook(t, root, request)
	draftState, _ := os.ReadFile(statePath)
	if !bytes.Equal(afterPre, draftState) || !strings.Contains(out, manifestPath) {
		t.Fatalf("draft save changed Runtime or omitted the exact remaining answer: %s", out)
	}
	fillS10QuestionnaireAnswer(t, manifestPath, "acceptance", "pass")
	answer := readS10QuestionnaireState(t, manifestPath)
	first := answer["coverage_inventory"].([]any)[0].(map[string]any)
	first["disposition"] = "unknown"
	first["oracle"] = "Synthetic fixture: this observation is not finished yet."
	pending, _ := json.MarshalIndent(answer, "", "  ")
	if err := os.WriteFile(manifestPath, pending, 0o644); err != nil {
		t.Fatal(err)
	}
	out = callS10QuestionnaireHook(t, root, request)
	pendingState := readS10QuestionnaireState(t, statePath)
	if pendingState["lifecycle"].(map[string]any)["state"] != "acceptance" || !hasS10QuestionnaireEvidence(root, pendingState, "acceptance", "pass") {
		t.Fatalf("explicitly unfinished observation was not recorded without completion: %s", out)
	}
	out = callS10QuestionnaireHook(t, root, request)
	replayed := readS10QuestionnaireState(t, statePath)
	if replayed["revision"] != pendingState["revision"] || len(replayed["evidence"].([]any)) != len(pendingState["evidence"].([]any)) {
		t.Fatalf("current same-answer replay wrote: %s", out)
	}
	// A real professional supplement is recorded even while its original
	// completion outlet waits for an actual clean-round condition. This is a
	// synthetic fixture adjustment, not a budget failure or an author procedure.
	fillS10QuestionnaireAnswer(t, manifestPath, "acceptance", "pass")
	waiting := readS10QuestionnaireState(t, statePath)
	waiting["review"].(map[string]any)["plan"].(map[string]any)["status"] = "running"
	waitingBytes, err := json.MarshalIndent(waiting, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(statePath, waitingBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	out = callS10QuestionnaireHook(t, root, request)
	recordedWaiting := readS10QuestionnaireState(t, statePath)
	if recordedWaiting["lifecycle"].(map[string]any)["state"] != "acceptance" {
		t.Fatalf("missing clean-round condition was bypassed: %s", out)
	}
	countBeforeContinuation := len(recordedWaiting["evidence"].([]any))
	recordedWaiting["review"].(map[string]any)["plan"].(map[string]any)["status"] = "clean"
	restoredBytes, err := json.MarshalIndent(recordedWaiting, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(statePath, restoredBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	// The same normal save finishes the pending original outlet without a
	// second evidence registration or a separate progression command.
	out = callS10QuestionnaireHook(t, root, request)
	if got := len(readS10QuestionnaireState(t, statePath)["evidence"].([]any)); got != countBeforeContinuation {
		t.Fatalf("same received report registered again: %d -> %d; %s", countBeforeContinuation, got, out)
	}
	if !strings.Contains(out, manifestPath) || !strings.Contains(out, "recorded at") {
		t.Fatalf("valid S10 answer was not registered at its exact path: %s", out)
	}
	state := readS10QuestionnaireState(t, statePath)
	if state["lifecycle"].(map[string]any)["state"] != "release_audit" {
		t.Fatalf("acceptance save did not complete exactly its own TR-015: %#v; %s", state["lifecycle"], out)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(manifestPath), "release_audit-manifest.json")); err != nil {
		t.Fatalf("TR-015 did not prepare the next exact questionnaire: %v", err)
	}
	var recorded map[string]any
	for _, raw := range state["evidence"].([]any) {
		entry, _ := raw.(map[string]any)
		if entry != nil && entry["kind"] == "acceptance" {
			recorded = entry
		}
	}
	if recorded == nil {
		t.Fatalf("valid save did not use the existing Runtime evidence consumer: %s", out)
	}
	if producers, _ := recorded["produced_by"].([]any); len(producers) != 1 || producers[0] != "Main" {
		t.Fatalf("normal Main Hook without platform agent_id producer = %#v", recorded["produced_by"])
	}
	revision, evidenceCount := state["revision"], len(state["evidence"].([]any))

	out = callS10QuestionnaireHook(t, root, request)
	state = readS10QuestionnaireState(t, statePath)
	if state["revision"] != revision || len(state["evidence"].([]any)) != evidenceCount {
		t.Fatalf("exact saved-answer replay wrote another evidence row or Runtime revision: %s", out)
	}
}

func s10QuestionnaireHookRoot(t *testing.T) (string, string) {
	t.Helper()
	root := newUXTestRoot(t, map[string]string{"REQ-AC-001.md": "# REQ-AC-001\n"})
	var stdout, stderr bytes.Buffer
	if code := cli.Run([]string{"init", "--root", root}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("init S10 hook fixture: code=%d stderr=%s", code, stderr.String())
	}
	state := readS10QuestionnaireState(t, filepath.Join(root, ".claude", "loop-state.json"))
	state["runtime_id"] = "loop-test"
	state["lifecycle"] = map[string]any{"state": "verification", "phase": "clean", "phase_revision": 0}
	state["baseline"] = map[string]any{"generation": 1, "captured_at": "2026-10-15T00:00:00Z"}
	reqPath := "docs/requirements/REQ-AC-001.md"
	req := []byte("# REQ-AC-001\n")
	writeS10QuestionnaireFile(t, root, reqPath, req)
	state["bound_req"] = map[string]any{
		"id": "REQ-001", "path": reqPath, "version": "v1.0.0", "sha256": sha256Hex(req),
		"status": "locked", "approved_by": "review-owner", "approved_at": "2026-10-15T00:00:00Z",
		"metadata": map[string]any{"ui_impact": "none"},
	}
	contractPath := "docs/dev/contracts/CONTRACT-001.md"
	contract := []byte("# CONTRACT-001\n")
	writeS10QuestionnaireFile(t, root, contractPath, contract)
	state["documents"] = []any{map[string]any{
		"id": "CONTRACT-001", "kind": "contract", "path": contractPath, "version": "v1.0.0",
		"sha256": sha256Hex(contract), "status": "locked", "generation": 1,
	}}
	planPath := ".claude/review/plans/review-plan-s10-questionnaire.json"
	plan := []byte(`{"review_plan_id":"review-plan-s10-questionnaire","review_round":1,"baseline_generation":1,"claims":[{"claim_id":"AUDIT-001"}]}`)
	writeS10QuestionnaireFile(t, root, planPath, plan)
	review := state["review"].(map[string]any)
	review["round"] = 1
	review["clean_round"] = nil
	review["plan"] = map[string]any{
		"plan_id": "review-plan-s10-questionnaire", "path": planPath, "sha256": sha256Hex(plan),
		"revision": 1, "review_round": 1, "status": "running",
		"e2e_coverage_state": "not_applicable", "submitted_at": "2026-10-15T00:00:00Z",
	}
	completionPath := ".claude/evidence/s10-questionnaire-completion.json"
	writeS10QuestionnaireFile(t, root, "internal/service.go", []byte("package service\n"))
	completion := []byte(`{"changed_paths":["internal/service.go"]}`)
	writeS10QuestionnaireFile(t, root, completionPath, completion)
	evidence := []any{map[string]any{
		"id": "completion-s10-questionnaire", "kind": "completion_report", "path": completionPath,
		"sha256": sha256Hex(completion), "status": "valid", "baseline_generation": 1, "review_round": 1,
		"produced_by": []any{"Main"}, "invalidated_by": nil, "invalidation_rule": nil,
		"invalidation_reason": nil, "responsibility_id": "S7", "scope_refs": []any{},
	}}
	for _, id := range []string{
		"ev:REQ-AC-001", "ev:CONTRACT-001", "ev:PATH-001", "ev:AUDIT-001",
		"ev:counter:REQ-AC-001", "ev:counter:CONTRACT-001", "ev:counter:PATH-001", "ev:counter:AUDIT-001",
	} {
		proofPath := ".claude/evidence/" + strings.ReplaceAll(id, ":", "-") + ".json"
		proof := []byte("supporting evidence")
		writeS10QuestionnaireFile(t, root, proofPath, proof)
		evidence = append(evidence, map[string]any{
			"id": id, "kind": "document_review", "path": proofPath, "sha256": sha256Hex(proof),
			"status": "valid", "baseline_generation": 1, "review_round": 1,
			"produced_by": []any{"S10 reviewer"}, "invalidated_by": nil, "invalidation_rule": nil,
			"invalidation_reason": nil, "responsibility_id": "S10", "scope_refs": []any{},
		})
	}
	for _, area := range []string{"state_machine", "transaction_uow", "concurrency_idempotency", "data_migration", "call_sites_topology", "observability_errors", "verification_evidence", "docs_release_scope"} {
		id := "ev:area:" + area
		proofPath := ".claude/evidence/" + strings.ReplaceAll(id, ":", "-") + ".json"
		proof := []byte("release-audit evidence for " + area)
		writeS10QuestionnaireFile(t, root, proofPath, proof)
		evidence = append(evidence, map[string]any{
			"id": id, "kind": "document_review", "path": proofPath, "sha256": sha256Hex(proof),
			"status": "valid", "baseline_generation": 1, "review_round": 1,
			"produced_by": []any{"S10 reviewer"}, "invalidated_by": nil, "invalidation_rule": nil,
			"invalidation_reason": nil, "responsibility_id": "S10", "scope_refs": []any{},
		})
	}
	state["evidence"] = evidence
	// Exercise the real Controller's committed development source rather than
	// bypassing its REQ authority boundary with a disk-only fixture.
	// newUXTestRoot already initialized this repository on feature/req. `git
	// init -b` on an existing repository does not move that existing HEAD, so
	// create the exact development ref that the real Controller reads.
	runGit(t, root, "checkout", "-B", "develop")
	runGit(t, root, "config", "user.name", "S10 Questionnaire Test")
	runGit(t, root, "config", "user.email", "s10@example.invalid")
	runGit(t, root, "add", reqPath, contractPath, "internal/service.go")
	runGit(t, root, "commit", "-m", "S10 prior-stage product fixture")
	runGit(t, root, "branch", "-f", "main", "develop")
	devCommit := recheckGitOutput(t, root, "rev-parse", "--verify", "refs/heads/develop^{commit}")
	mainCommit := recheckGitOutput(t, root, "rev-parse", "--verify", "refs/heads/main^{commit}")
	if devCommit != mainCommit {
		t.Fatalf("S10 fixture refs diverged: develop=%s main=%s", devCommit, mainCommit)
	}
	state["bound_req"].(map[string]any)["workspace"] = map[string]any{
		"project_root": root, "dev_branch": "develop", "release_upstream": "main",
		"bound_commit": devCommit,
	}
	stateData, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".claude", "loop-state.json"), append(stateData, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	// Synthetic upstream only: the original clean-round producer/guard and
	// original Controller transition prepare S10, not an S10 passive Pre.
	callS10QuestionnaireHook(t, root, map[string]any{"hook_event_name": "PreToolUse", "cwd": root, "tool_name": "Read"})
	unready := readS10QuestionnaireState(t, filepath.Join(root, ".claude", "loop-state.json"))
	if unready["lifecycle"].(map[string]any)["state"] != "verification" {
		t.Fatal("missing actual clean-round precondition was bypassed")
	}
	seedS10QuestionnaireCleanRound(t, root)
	out := callS10QuestionnaireHook(t, root, map[string]any{"hook_event_name": "PreToolUse", "cwd": root, "tool_name": "Read"})
	entered := readS10QuestionnaireState(t, filepath.Join(root, ".claude", "loop-state.json"))
	if entered["lifecycle"].(map[string]any)["state"] != "acceptance" {
		t.Fatalf("synthetic S7 clean round did not pass the original TR-009: %s", out)
	}
	return root, filepath.Join(root, "s10", "loop-test", "g1", "r1", "acceptance-manifest.json")
}

func callS10QuestionnaireHook(t *testing.T, root string, payload map[string]any) string {
	t.Helper()
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	event, _ := payload["hook_event_name"].(string)
	if code := cli.Run([]string{"hook", "--event", event, "--root", root}, bytes.NewReader(data), &stdout, &stderr); code != 0 {
		t.Fatalf("%s S10 Hook: code=%d stderr=%s stdout=%s", event, code, stderr.String(), stdout.String())
	}
	return stdout.String()
}

func readS10QuestionnaireState(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var state map[string]any
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatal(err)
	}
	return state
}

func writeS10QuestionnaireFile(t *testing.T, root, relative string, data []byte) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func fillS10QuestionnaireAnswer(t *testing.T, path, kind, outcome string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var manifest map[string]any
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatalf("decode prepared %s questionnaire: %v", kind, err)
	}
	manifest["outcome"] = outcome
	if outcome == "blocked" {
		manifest["blocking_findings"] = []any{map[string]any{"id": "BLOCK-S10-QUESTIONNAIRE", "route": "TR-018"}}
	}
	coverage, _ := manifest["coverage_inventory"].([]any)
	for _, raw := range coverage {
		row, _ := raw.(map[string]any)
		if row == nil {
			continue
		}
		row["oracle"] = "Synthetic fixture observation: the supplied material is readable and its prior result is reused; this is not native product validation."
		row["evidence_refs"] = []string{}
		row["disposition"] = "pass"
	}
	counterevidence, _ := manifest["counterevidence"].([]any)
	for _, raw := range counterevidence {
		row, _ := raw.(map[string]any)
		if row == nil {
			continue
		}
		inventoryID, _ := row["inventory_id"].(string)
		row["question"] = "What current evidence could disprove the scoped conclusion for " + inventoryID + "?"
		row["evidence_refs"] = []string{"ev:counter:AUDIT-001"}
		row["outcome"] = "pass"
	}
	if kind == "release_audit" {
		if len(coverage) != 1 || coverage[0].(map[string]any)["id"] != "acceptance" {
			t.Fatalf("audit copied the acceptance inventory or created eight required tasks: %#v", coverage)
		}
		areas, _ := manifest["audit_areas"].([]any)
		if len(areas) != 0 {
			t.Fatal("fresh release-audit questionnaire asks for duplicate audit summary answers")
		}
	}
	data, err = json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}

func seedS10QuestionnaireCleanRound(t *testing.T, root string) {
	t.Helper()
	statePath := filepath.Join(root, ".claude", "loop-state.json")
	state := readS10QuestionnaireState(t, statePath)
	review, _ := state["review"].(map[string]any)
	plan, _ := review["plan"].(map[string]any)
	if review == nil || plan == nil {
		t.Fatal("S10 Hook fixture must retain its pinned S7 ReviewPlan")
	}
	plan["status"] = "clean"
	review["claims"] = map[string]any{
		"AUDIT-001": map[string]any{
			"lens": "qa", "applicability": "required", "disposition": "pass",
			"assignment_id": "assignment-qa-s10-questionnaire", "result_id": "review-result-s10-questionnaire",
			"finding_ids": []any{},
		},
	}
	review["clean_round"] = float64(1)
	encoded, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(statePath, append(encoded, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	pinnedPlan, _, err := reviewpkg.LoadPlan(root, state)
	if err != nil {
		t.Fatalf("load the exact pinned S7 ReviewPlan for the clean-round producer: %v", err)
	}
	cleanRound := map[string]any{
		"schema_version": "1.0.0", "clean_round_id": "clean-round-s10-questionnaire",
		"evidence_id": "clean-round-s10-questionnaire", "kind": "clean_round", "runtime_id": "loop-test",
		"producer_agent_id": "round-consumer", "producer_responsibility": "Clean Round Evaluator",
		"review_plan_id": "review-plan-s10-questionnaire", "review_round": 1,
		"baseline_generation": 1, "subject_digest": reviewpkg.SubjectDigest(pinnedPlan), "result_refs": []any{},
		"conclusion": "pass", "evaluated_at": "2026-10-15T00:00:00Z",
		"evaluated_by": "round-consumer", "evaluator_version": "review.SubmitResult/1",
	}
	artifact, err := json.Marshal(cleanRound)
	if err != nil {
		t.Fatal(err)
	}
	const relative = ".claude/evidence/clean-round-s10-questionnaire.json"
	writeS10QuestionnaireFile(t, root, relative, artifact)
	if _, err := runtime.RecordEvidence(root, statePath, filepath.Join(root, ".claude", "loop-events.jsonl"), runtime.EvidenceRequest{
		ExpectedRevision: -1, ID: "clean-round-s10-questionnaire", Kind: "clean_round", Path: relative,
		ProducedBy: []string{"round-consumer"}, ResponsibilityID: "Clean Round Evaluator", ReviewRound: s10IntPointer(1),
		Validator: semantic.RuntimeCandidateValidator{},
	}); err != nil {
		t.Fatalf("record S7 clean-round precondition through the existing evidence consumer: %v", err)
	}
}

func s10IntPointer(value int) *int { return &value }
