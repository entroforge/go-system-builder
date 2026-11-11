package runtime_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/entroforge/go-system-builder/internal/fileview"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

func TestTaskPlanningArtifactsReturnsExactREQBatchIncludingCancelled(t *testing.T) {
	root, state, files := taskPlanningRuntimeFixture(t)
	inputs, err := runtime.ResolveTaskPlanningInputs(root, state, files)
	if err != nil {
		t.Fatal(err)
	}
	binding, err := runtime.CurrentTaskPlanningBinding(state)
	if err != nil {
		t.Fatal(err)
	}
	p, err := workpacket.BuildTaskPlanning(binding, inputs)
	if err != nil {
		t.Fatal(err)
	}
	writeTaskPlanningFile(t, root, "docs/dev/tasks/TASK-REQ-001-01.md", "> Status: complete\n> Version: v1.0.0\n> Source REQ refs: REQ-001\n")
	writeTaskPlanningFile(t, root, "docs/dev/tasks/TASK-REQ-001-02.md", "> Status: cancelled\n> Version: v1.0.0\n> Source REQ refs: REQ-001\n")
	writeTaskPlanningFile(t, root, "docs/dev/tasks/index-REQ-001.md", "# REQ-001 dispatch\n> REQ: REQ-001\n> Dispatch policy: waves-v1\n> Revision: 1\n> Status: complete\n\n## W1 First wave\n\n- [ ] [TASK-REQ-001-01](TASK-REQ-001-01.md)\n")
	answer := workpacket.TaskPlanningAnswer{
		Conclusion: stringPtr("pass"), IndexPath: stringPtr("docs/dev/tasks/index-REQ-001.md"),
		TaskPaths: []string{"docs/dev/tasks/TASK-REQ-001-02.md", "docs/dev/tasks/TASK-REQ-001-01.md"},
	}
	refs, err := runtime.TaskPlanningArtifacts(root, state, p, answer, files)
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 3 || refs[0].Path != "docs/dev/tasks/index-REQ-001.md" || refs[1].Path != "docs/dev/tasks/TASK-REQ-001-01.md" || refs[2].Path != "docs/dev/tasks/TASK-REQ-001-02.md" {
		t.Fatalf("artifacts must be index first then all sorted tasks: %#v", refs)
	}
}

func TestTaskPlanningArtifactsDoesNotRepeatProfessionalBatchCheck(t *testing.T) {
	root, state, files := taskPlanningRuntimeFixture(t)
	inputs, err := runtime.ResolveTaskPlanningInputs(root, state, files)
	if err != nil {
		t.Fatal(err)
	}
	binding, _ := runtime.CurrentTaskPlanningBinding(state)
	p, err := workpacket.BuildTaskPlanning(binding, inputs)
	if err != nil {
		t.Fatal(err)
	}
	writeTaskPlanningFile(t, root, "docs/dev/tasks/TASK-REQ-001-01.md", "> Status: complete\n> Source REQ refs: REQ-001\n")
	writeTaskPlanningFile(t, root, "docs/dev/tasks/TASK-REQ-001-02.md", "> Status: cancelled\n> Source REQ refs: REQ-001\n")
	writeTaskPlanningFile(t, root, "docs/dev/tasks/index-REQ-001.md", "# REQ-001 dispatch\n> REQ: REQ-001\n> Dispatch policy: waves-v1\n> Revision: 1\n> Status: complete\n\n## W1\n- [ ] [TASK-REQ-001-01](TASK-REQ-001-01.md)\n")
	answer := workpacket.TaskPlanningAnswer{Conclusion: stringPtr("pass"), IndexPath: stringPtr("docs/dev/tasks/index-REQ-001.md"), TaskPaths: []string{"docs/dev/tasks/TASK-REQ-001-01.md"}}
	if refs, err := runtime.TaskPlanningArtifacts(root, state, p, answer, files); err != nil || len(refs) != 3 {
		t.Fatalf("system omitted unlisted cancelled task: refs=%v err=%v", refs, err)
	}
	answer.TaskPaths = append(answer.TaskPaths, "docs/dev/tasks/TASK-REQ-001-02.md")
	writeTaskPlanningFile(t, root, "docs/dev/tasks/index-REQ-001.md", "# REQ-001 dispatch\n> REQ: REQ-001\n> Dispatch policy: waves-v1\n> Revision: 1\n> Status: complete\n\n## W1\n")
	if _, err := runtime.TaskPlanningArtifacts(root, state, p, answer, files); err != nil {
		t.Fatalf("artifact projection repeated dispatch-index check: %v", err)
	}
}

func TestTaskPlanningPacketKeepsOriginalResponsibilityAcrossUnrelatedChanges(t *testing.T) {
	t.Run("extra contract", func(t *testing.T) {
		root, state, files := taskPlanningRuntimeFixture(t)
		inputs, err := runtime.ResolveTaskPlanningInputs(root, state, files)
		if err != nil {
			t.Fatal(err)
		}
		binding, _ := runtime.CurrentTaskPlanningBinding(state)
		p, err := workpacket.BuildTaskPlanning(binding, inputs)
		if err != nil {
			t.Fatal(err)
		}
		writeTaskPlanningFile(t, root, "docs/dev/contracts/FE-002.md", "> Status: locked\n> Version: v1.0.0\n")
		if err := runtime.ValidateTaskPlanningPacket(root, state, p, files); err != nil {
			t.Fatalf("unregistered extra contract invalidated issued responsibility: %v", err)
		}
	})
	t.Run("design drift", func(t *testing.T) {
		root, state, files := taskPlanningRuntimeFixture(t)
		inputs, err := runtime.ResolveTaskPlanningInputs(root, state, files)
		if err != nil {
			t.Fatal(err)
		}
		binding, _ := runtime.CurrentTaskPlanningBinding(state)
		p, err := workpacket.BuildTaskPlanning(binding, inputs)
		if err != nil {
			t.Fatal(err)
		}
		writeTaskPlanningFile(t, root, "docs/architecture/ARCHITECTURE-001.md", "> Status: locked\n> Version: v1.0.0\nchanged\n")
		if err := runtime.ValidateTaskPlanningPacket(root, state, p, files); err != nil {
			t.Fatalf("later design content invalidated issued responsibility: %v", err)
		}
	})
}

func TestTaskPlanningUsesRecordedS3ResponsibilityWithoutMachineEnvelopeIdentity(t *testing.T) {
	root, state, files := taskPlanningRuntimeFixture(t)
	evidencePath := ".claude/evidence/E-S3-001.json"
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(evidencePath)))
	if err != nil {
		t.Fatal(err)
	}
	var envelope map[string]any
	if err := json.Unmarshal(data, &envelope); err != nil {
		t.Fatal(err)
	}
	envelope["runtime_id"] = "runtime-other"
	mutated, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	writeBytes(t, root, evidencePath, mutated)
	inputs, err := runtime.ResolveTaskPlanningInputs(root, state, files)
	if err != nil || len(inputs.Contracts) == 0 {
		t.Fatalf("recorded original S3 sources were blocked by machine envelope metadata: %+v %v", inputs.Contracts, err)
	}
}

func TestTaskPlanningInputsProjectAcceptedS3SourcesIncludingSharedModels(t *testing.T) {
	root, state, files := taskPlanningRuntimeFixture(t)
	modelPath := "docs/models/REQ-001.json"
	modelBytes := []byte(`{"type":"object","title":"REQ-001 model"}`)
	writeBytes(t, root, modelPath, modelBytes)
	addTaskPlanningS3Subject(t, root, state, workpacket.Subject{Path: modelPath})
	state["documents"] = append(state["documents"].([]any), map[string]any{
		"id": "shared-model:" + modelPath, "kind": "design", "path": modelPath,
		"version": "", "status": "locked", "generation": 1,
	})

	inputs, err := runtime.ResolveTaskPlanningInputs(root, state, files)
	if err != nil {
		t.Fatal(err)
	}
	sources := map[string]workpacket.Subject{}
	for _, source := range inputs.Sources {
		sources[source.Path] = source
	}
	for _, path := range []string{
		"docs/architecture/ARCHITECTURE-001.md",
		"docs/design/decisions/ADR-001.md",
		modelPath,
	} {
		if _, ok := sources[path]; !ok {
			t.Fatalf("accepted S3 source %s was not projected into S4: %#v", path, inputs.Sources)
		}
	}
	if sources[modelPath].Version != "" {
		t.Fatalf("S3 shared-model subject was not preserved exactly: %+v", sources[modelPath])
	}

	writeBytes(t, root, modelPath, []byte(`{"type":"object","title":"changed"}`))
	if _, err := runtime.ResolveTaskPlanningInputs(root, state, files); err != nil {
		t.Fatalf("changed source content must not become a hash gate: %v", err)
	}
}

func TestTaskPlanningInputsAllowDeclaredForeignREQContractBatch(t *testing.T) {
	root, state, files := taskPlanningRuntimeFixture(t)
	writeTaskPlanningFile(t, root, "docs/dev/contracts/CONTRACTS-002.md", "# CONTRACTS-002\n> Requirement: REQ-002\n> Status: locked\n> Version: v1.0.0\n\n## 合同清单\n| Contract ID | Type | Name | Status | Version | Notes |\n|:---|:---|:---|:---|:---|:---|\n| BE-002 | backend | api | locked | v1.0.0 | foreign |\n")
	writeTaskPlanningFile(t, root, "docs/dev/contracts/BE-002.md", "> Status: locked\n> Version: v1.0.0\n")
	if _, err := runtime.ResolveTaskPlanningInputs(root, state, files); err != nil {
		t.Fatalf("declared foreign REQ contract batch blocked the accepted S3/S4 closure: %v", err)
	}

	writeTaskPlanningFile(t, root, "docs/dev/contracts/SYNC-999.md", "> Status: draft\n")
	inputs, err := runtime.ResolveTaskPlanningInputs(root, state, files)
	if err != nil {
		t.Fatalf("unowned draft blocked registered batch: %v", err)
	}
	for _, ref := range inputs.Contracts {
		if ref.Path == "docs/dev/contracts/SYNC-999.md" {
			t.Fatal("unowned draft was attributed to accepted S3 contracts")
		}
	}
}

func taskPlanningRuntimeFixture(t *testing.T, includeBoundREQ ...bool) (string, map[string]any, fileview.Reader) {
	t.Helper()
	root := t.TempDir()
	for _, dir := range []string{".claude/evidence", "docs/requirements", "docs/architecture", "docs/design/decisions", "docs/dev/contracts", "docs/dev/tasks", "docs/rules"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0700); err != nil {
			t.Fatal(err)
		}
	}
	reqPath := "docs/requirements/REQ-001.md"
	designPath := "docs/architecture/ARCHITECTURE-001.md"
	conditionalPath := "docs/design/decisions/ADR-001.md"
	indexPath := "docs/dev/contracts/CONTRACTS-001.md"
	contractPath := "docs/dev/contracts/FE-001.md"
	req := []byte("# REQ-001\n> Status: locked\n> Version: v1.0.0\n")
	design := []byte("# Architecture\n> Status: locked\n> Version: v1.0.0\n")
	conditional := []byte("# ADR-001\n> Status: accepted\n> Version: v1.0.0\n")
	index := []byte("# CONTRACTS-001\n> Requirement: REQ-001\n> Status: locked\n> Version: v1.0.0\n\n## 合同清单\n| Contract ID | Type | Name | Status | Version | Notes |\n|:---|:---|:---|:---|:---|:---|\n| FE-001 | frontend | ui | locked | v1.0.0 | entry |\n\n## 需求覆盖矩阵\n| Contract | Clause | Purpose |\n|:---|:---|:---|\n| FE-001 §1 | user view | visible |\n")
	contract := []byte("# FE-001\n> Status: locked\n> Version: v1.0.0\n\n| 本合同条款 | 承诺 |\n|---|---|\n| §1 | The user view is visible. |\n")
	taskTemplate := []byte("# Task template\n")
	dispatchRules := []byte("# Dispatch plan rules\n")
	writeBytes(t, root, reqPath, req)
	writeBytes(t, root, designPath, design)
	writeBytes(t, root, conditionalPath, conditional)
	writeBytes(t, root, indexPath, index)
	writeBytes(t, root, contractPath, contract)
	writeBytes(t, root, "docs/dev/tasks/TASK-template.md", taskTemplate)
	writeBytes(t, root, "docs/rules/dispatch-plan.md", dispatchRules)

	reqRef := workpacket.Reference{ID: "REQ-001", Path: reqPath, Version: "v1.0.0"}
	designRef := workpacket.Reference{ID: "ARCHITECTURE-001", Path: designPath, Version: "v1.0.0"}
	s3Binding := workpacket.PlanningBinding{RuntimeID: "loop-REQ-001", Generation: 1, StageEntry: 2, Stage: "S3", Owner: "Main", Requirement: reqRef}
	s3Packet, err := workpacket.BuildContracts(s3Binding, workpacket.ContractInputs{
		Designs:     []workpacket.Reference{designRef},
		Sources:     []workpacket.Subject{{Path: conditionalPath, Version: "v1.0.0"}},
		Directories: map[string][]string{"docs/design/decisions": {"ADR-001.md"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	answer := workpacket.ContractAnswer{
		Conclusion: stringPtr("pass"), IndexPath: stringPtr(indexPath), ContractPaths: []string{contractPath},
		Applicability: map[string]workpacket.ContractApplicability{
			"FE":   {Disposition: "required", Rationale: "The REQ includes a user-facing view."},
			"BE":   {Disposition: "not_applicable", Rationale: "This REQ has no backend behavior."},
			"SYNC": {Disposition: "not_applicable", Rationale: "This REQ has no shared integration behavior."},
		},
	}
	artifacts := []workpacket.Reference{
		{ID: "CONTRACTS-001", Path: indexPath, Version: "v1.0.0"},
		{ID: "FE-001", Path: contractPath, Version: "v1.0.0"},
	}
	s3Evidence, err := workpacket.ContractEvidence(s3Packet, "E-S3-001", artifacts, answer, time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	evidenceData, err := json.Marshal(s3Evidence)
	if err != nil {
		t.Fatal(err)
	}
	evidencePath := ".claude/evidence/E-S3-001.json"
	writeBytes(t, root, evidencePath, evidenceData)
	state := map[string]any{
		"documents": []any{
			map[string]any{"id": designRef.ID, "kind": "design", "path": designRef.Path, "version": designRef.Version, "status": "locked", "generation": 1},
			map[string]any{"id": "CONTRACTS-001", "kind": "contract", "path": indexPath, "version": "v1.0.0", "status": "locked", "generation": 1},
			map[string]any{"id": "FE-001", "kind": "contract", "path": contractPath, "version": "v1.0.0", "status": "locked", "generation": 1},
		},
	}
	if len(includeBoundREQ) > 0 && includeBoundREQ[0] {
		// Match the original bindREQ registration before the S3 Writers run.
		state["documents"] = append(state["documents"].([]any), map[string]any{
			"id": reqRef.ID, "kind": "req", "path": reqRef.Path, "version": reqRef.Version,
			"status": "locked", "generation": s3Binding.Generation,
		})
	}
	state = taskPlanningOriginalS3(t, root, state, s3Packet, answer, evidencePath)
	return root, state, fileview.Disk{Root: root}
}

func writeTaskPlanningFile(t *testing.T, root, path, content string) {
	t.Helper()
	writeBytes(t, root, path, []byte(content))
}

func addTaskPlanningS3Subject(t *testing.T, root string, state map[string]any, subject workpacket.Subject) {
	t.Helper()
	evidencePath := ".claude/evidence/E-S3-001.json"
	evidenceData, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(evidencePath)))
	if err != nil {
		t.Fatal(err)
	}
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(evidenceData, &envelope); err != nil {
		t.Fatal(err)
	}
	var subjects []workpacket.Subject
	if err := json.Unmarshal(envelope["subject_refs"], &subjects); err != nil {
		t.Fatal(err)
	}
	subjects = append(subjects, subject)
	envelope["subject_refs"], err = json.Marshal(subjects)
	if err != nil {
		t.Fatal(err)
	}
	updated, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	writeBytes(t, root, evidencePath, updated)
}

func writeBytes(t *testing.T, root, path string, data []byte) {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(full), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, data, 0600); err != nil {
		t.Fatal(err)
	}
}

func stringPtr(value string) *string { return &value }

// Only output-directory enumeration, no frozen input access or Verify, projects
// the actual output bytes already professionally checked by the Service.
type taskOutputReader struct {
	fileview.Reader
	reads map[string]int
}

func (f taskOutputReader) ReadFile(path string) ([]byte, error) {
	f.reads[path]++
	if f.reads[path] > 1 {
		return nil, fmt.Errorf("repeated artifact read: %s", path)
	}
	return f.Reader.ReadFile(path)
}
func (f taskOutputReader) ReadDir(path string) ([]os.DirEntry, error) {
	if path != "docs/dev/tasks" {
		return nil, fmt.Errorf("unexpected source enumeration: %s", path)
	}
	return f.Reader.ReadDir(path)
}
func (f taskOutputReader) Verify() error { return fmt.Errorf("unexpected source proof") }

func TestTaskPlanningArtifactsReadsOnlyActualOutputsOnce(t *testing.T) {
	root, state, files := taskPlanningRuntimeFixture(t)
	inputs, err := runtime.ResolveTaskPlanningInputs(root, state, files)
	if err != nil {
		t.Fatal(err)
	}
	binding, _ := runtime.CurrentTaskPlanningBinding(state)
	p, err := workpacket.BuildTaskPlanning(binding, inputs)
	if err != nil {
		t.Fatal(err)
	}
	index, task := "docs/dev/tasks/index-REQ-001.md", "docs/dev/tasks/TASK-REQ-001-01.md"
	writeTaskPlanningFile(t, root, index, "# Actual index\n")
	writeTaskPlanningFile(t, root, task, "> Status: complete\n> Source REQ refs: REQ-001\n")
	reader := taskOutputReader{Reader: files, reads: map[string]int{}}
	answer := workpacket.TaskPlanningAnswer{Conclusion: stringPtr("pass"), IndexPath: &index, TaskPaths: []string{task}}
	refs, err := runtime.TaskPlanningArtifacts(root, state, p, answer, reader)
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 2 || len(reader.reads) != 2 || reader.reads[index] != 1 || reader.reads[task] != 1 {
		t.Fatalf("unexpected projection reads: %v", reader.reads)
	}
	envelope, err := workpacket.TaskPlanningEvidence(p, "E-S4", refs, answer, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(envelope)
	// Inputs need not be rebuilt or re-proved to decode an existing record.
	p.Inputs = workpacket.TaskPlanningInputs{}
	if err := runtime.ValidateTaskPlanningEnvelope(p, "E-S4", answer, data); err != nil {
		t.Fatal(err)
	}
	envelope["task_planning_answer"] = map[string]any{"conclusion": "pass"}
	data, _ = json.Marshal(envelope)
	if err := runtime.ValidateTaskPlanningEnvelope(p, "E-S4", answer, data); err == nil {
		t.Fatal("unreadable professional record accepted")
	}
}

func TestTaskPlanningIgnoresUnrelatedContractDirectoryMembersAndPreservesIssuedPacket(t *testing.T) {
	root, state, files := taskPlanningRuntimeFixture(t)
	inputs, err := runtime.ResolveTaskPlanningInputs(root, state, files)
	if err != nil {
		t.Fatal(err)
	}
	// Historical packets froze the entire directory. Keep their original ID and
	// metadata while applying the current accepted-contract scope on validation.
	entries, err := files.ReadDir("docs/dev/contracts")
	if err != nil {
		t.Fatal(err)
	}
	names := []string{}
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	inputs.Directories["docs/dev/contracts"] = names
	binding, _ := runtime.CurrentTaskPlanningBinding(state)
	p, err := workpacket.BuildTaskPlanning(binding, inputs)
	if err != nil {
		t.Fatal(err)
	}
	files = commitContractBatchFixture(t, contractBatchFixture{root: root})
	for _, path := range []string{"local-note.txt", "notes.md", ".BE-001.md.swp", "scratch/FE-999.md"} {
		writeTaskPlanningFile(t, root, "docs/dev/contracts/"+path, "# Notes about BE-039\nLocal working notes.\n")
	}
	for name, fence := range map[string]string{"example-notes.md": "```", "tilde-notes.md": "~~~"} {
		writeTaskPlanningFile(t, root, "docs/dev/contracts/"+name, "Local notes with no document title.\n\n"+fence+"markdown\n# BE-039 Example contract\n"+fence+"\n")
	}
	if _, err := runtime.ResolveTaskPlanningInputs(root, state, files); err != nil {
		t.Fatalf("unrelated members blocked S4 issue: %v", err)
	}
	if err := runtime.ValidateTaskPlanningPacket(root, state, p, files); err != nil {
		t.Fatalf("unrelated members invalidated historical S4 packet: %v", err)
	}
	descriptivePath := "docs/dev/contracts/service-contract.md"
	writeTaskPlanningFile(t, root, descriptivePath, "```markdown\n# Example only\n```\n\n# BE-999 Service contract\n> Status: draft\n")
	if err := runtime.ValidateTaskPlanningPacket(root, state, p, files); err != nil {
		t.Fatalf("later draft invalidated original issued packet: %v", err)
	}
	if err := os.Remove(filepath.Join(root, descriptivePath)); err != nil {
		t.Fatal(err)
	}
	writeTaskPlanningFile(t, root, "docs/dev/contracts/FE-999.md", "# FE-999\n> Status: draft\n")
	if err := runtime.ValidateTaskPlanningPacket(root, state, p, files); err != nil {
		t.Fatalf("unregistered contract candidate invalidated issued packet: %v", err)
	}
}

// The first read validates the registered contract; a later read must not
// silently provide a new clause universe from bytes that were never accepted.
type changedTaskIndexReader struct {
	fileview.Reader
	path  string
	reads int
}

func (r *changedTaskIndexReader) ReadFile(path string) ([]byte, error) {
	data, err := r.Reader.ReadFile(path)
	if path == r.path {
		r.reads++
		if r.reads >= 2 {
			return append(data, []byte("\nBE-001 §999\n")...), err
		}
	}
	return data, err
}
func TestTaskPlanningUsesOriginalIndexReadForClauseProjection(t *testing.T) {
	root, state, files := taskPlanningRuntimeFixture(t)
	reader := &changedTaskIndexReader{Reader: files, path: "docs/dev/contracts/CONTRACTS-001.md"}
	inputs, err := runtime.ResolveTaskPlanningInputs(root, state, reader)
	if err != nil || reader.reads != 1 {
		t.Fatalf("original index read: reads=%d err=%v", reader.reads, err)
	}
	for _, clause := range inputs.ClauseUniverse {
		if clause.Section == 999 {
			t.Fatal("later unrequested index content entered the clause universe")
		}
	}
}

func TestS4OriginalS3SourcesIgnoreUnrelatedHistoricalMachineFields(t *testing.T) {
	values := []any{nil, "old-value", float64(7), []any{"old"}, map[string]any{"old": true}}
	for index, value := range values {
		t.Run(fmt.Sprintf("variant-%d", index), func(t *testing.T) {
			root, state, files := taskPlanningRuntimeFixture(t)
			path := filepath.Join(root, ".claude/evidence/E-S3-001.json")
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var envelope map[string]any
			if err := json.Unmarshal(data, &envelope); err != nil {
				t.Fatal(err)
			}
			envelope["old_machine_field"] = value
			envelope["runtime_id"] = value
			if subjects, ok := envelope["subject_refs"].([]any); ok && len(subjects) > 0 {
				subjects[0].(map[string]any)["sha256"] = value
			}
			data, err = json.Marshal(envelope)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
			for _, raw := range state["evidence"].([]any) {
				row := raw.(map[string]any)
				if row["id"] == "E-S3-001" {
					row["review_round"] = value
					delete(row, "invalidated_by")
					delete(row, "invalidation_rule")
					delete(row, "invalidation_reason")
				}
			}
			inputs, err := runtime.ResolveTaskPlanningInputs(root, state, files)
			if err != nil || len(inputs.Contracts) == 0 {
				t.Fatalf("original S3 source blocked by unrelated machine field: %+v %v", inputs.Contracts, err)
			}
			if after, err := os.ReadFile(path); err != nil || string(after) != string(data) {
				t.Fatalf("original evidence changed: %v", err)
			}
		})
	}
}
