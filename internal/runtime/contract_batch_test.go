package runtime_test

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/entroforge/go-system-builder/internal/fileview"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

func TestContractArtifactsReturnsExactLockedBatch(t *testing.T) {
	f := newContractBatchFixture(t)
	refs, err := runtime.ContractArtifacts(f.root, f.state, f.packet, f.answer, f.files)
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 2 || refs[0].Path != f.indexPath || refs[1].Path != f.contractPath {
		t.Fatalf("artifact order/set = %#v", refs)
	}
	if refs[0].ID != "CONTRACTS-001" || refs[1].ID != "BE-001" || refs[0].Version != "v1.0.0" || refs[1].Version != "v1.0.0" {
		t.Fatalf("artifact references = %#v", refs)
	}
	if refs[0].SHA256 != workpacket.Digest(readContractFile(t, f.root, f.indexPath)) || refs[1].SHA256 != workpacket.Digest(readContractFile(t, f.root, f.contractPath)) {
		t.Fatal("artifact SHA does not describe checkout content")
	}
	for _, display := range []string{"REQ-001（v1.0.0，当前需求）", "`REQ-001`", "[REQ-001](../../requirements/REQ-001.md)（current）"} {
		original := string(readContractFile(t, f.root, f.indexPath))
		presented := strings.Replace(original, "> 需求：REQ-001", "> 需求："+display, 1)
		presented = strings.Replace(presented, "| BE-001 |", "| [BE-001](BE-001.md) |", 1)
		writeContractFile(t, f.root, f.indexPath, presented)
		if _, err := runtime.ContractArtifacts(f.root, f.state, f.packet, f.answer, f.files); err != nil {
			t.Fatalf("natural index display %q rejected: %v", display, err)
		}
		writeContractFile(t, f.root, f.indexPath, original)
	}

	// The real booking index keeps applicability tables after the inventory,
	// and displays an inapplicable FE row without inventing a FE document.
	natural := newContractBatchFixture(t)
	index := string(readContractFile(t, natural.root, natural.indexPath))
	index = strings.Replace(index, "| BE-001 | 后端 | Builder | locked | v1.0.0 | N/A |", "| FE-001 | frontend — **`not_applicable`** | 不适用（本 REQ 无前端交付物） | 无文档（判定见下节） | — | 无 |\n| BE-001 | backend | Builder | locked | v1.0.0 | SYNC-001 |\n| SYNC-001 | 联调/线缆 | Builder | locked | v1.0.0 | 无前端消费者 |", 1)
	index = strings.Replace(index, "\n## 联调点矩阵", "\n**适用性判定**\n\n| 合同 | disposition | 依据 |\n|:---|:---|:---|\n| FE-001 | not_applicable | 无前端交付物 |\n\n### FE-001 不适用判定\n\n| 项 | 结论 | 依据 |\n|:---|:---|:---|\n| 合同文档 | 无 | 真实专业不适用 |\n\n## 联调点矩阵", 1)
	index = strings.Replace(index, "|:---|:---|:---|:---|:---|\n\n## 需求覆盖矩阵", "|:---|:---|:---|:---|:---|\n| SYNC-001 | 不适用 | BE-001 | SYNC-001 | locked |\n\n## 需求覆盖矩阵", 1)
	writeContractFile(t, natural.root, natural.indexPath, index)
	writeContractFile(t, natural.root, "docs/dev/contracts/SYNC-001.md", "# Wire contract\n> 状态：locked\n> 版本：v1.0.0\n")
	natural.answer.ContractPaths = append(natural.answer.ContractPaths, "docs/dev/contracts/SYNC-001.md")
	natural.answer.Applicability["SYNC"] = workpacket.ContractApplicability{Disposition: "required", Rationale: "实际 HTTP 线缆契约"}
	got, err := runtime.ContractArtifacts(natural.root, natural.state, natural.packet, natural.answer, natural.files)
	if err != nil || len(got) != 3 {
		t.Fatalf("natural booking inventory/N-A and display text rejected: %#v %v", got, err)
	}

}

func TestContractArtifactsAppendsCurrentSharedModelCheckerClosure(t *testing.T) {
	f := newContractBatchFixture(t)
	prepareContractSharedModelBatch(t, f)

	refs, err := runtime.ContractArtifacts(f.root, f.state, f.packet, f.answer, f.files)
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 5 || refs[0].Path != f.indexPath || refs[1].Path != f.contractPath {
		t.Fatalf("artifact refs = %#v", refs)
	}
	wantModel := map[string]bool{
		"docs/architecture/data-model/order.schema.json":  true,
		"docs/architecture/data-model/order-valid.json":   true,
		"docs/architecture/data-model/order-invalid.json": true,
	}
	for _, ref := range refs[2:] {
		if !wantModel[ref.Path] || ref.Version != "" || ref.SHA256 != workpacket.Digest(readContractFile(t, f.root, ref.Path)) {
			t.Errorf("shared-model ref = %#v", ref)
		}
		delete(wantModel, ref.Path)
	}
	if len(wantModel) != 0 {
		t.Fatalf("shared-model closure omitted %v", wantModel)
	}
	changedSchema := append(readContractFile(t, f.root, "docs/architecture/data-model/order.schema.json"), '\n')
	view := replacingReader{Reader: f.files, path: "docs/architecture/data-model/order.schema.json", data: changedSchema}
	if _, err := runtime.ContractArtifacts(f.root, f.state, f.packet, f.answer, view); err == nil || !strings.Contains(err.Error(), "differs from original checkout") {
		t.Fatalf("shared-model source view drift was accepted: %v", err)
	}
	evidence, err := workpacket.ContractEvidence(f.packet, "E-S3-1", refs, f.answer, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	evidenceBytes, err := json.Marshal(evidence)
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.ValidateContractEnvelope(f.packet, "E-S3-1", f.answer, evidenceBytes); err != nil {
		t.Fatalf("historical envelope did not reconstruct SHA-only model refs: %v", err)
	}
	subjects := evidence["subject_refs"].([]workpacket.Subject)
	for path := range map[string]bool{
		"docs/architecture/data-model/order.schema.json":  true,
		"docs/architecture/data-model/order-valid.json":   true,
		"docs/architecture/data-model/order-invalid.json": true,
	} {
		found := false
		for _, subject := range subjects {
			if subject.Path == path && subject.Version == "" {
				found = true
			}
		}
		if !found {
			t.Errorf("evidence omitted SHA-only shared-model subject %s", path)
		}
	}
}

func TestContractArtifactsDoesNotRepeatSharedModelRefAlreadyFrozenUpstream(t *testing.T) {
	f := newContractBatchFixture(t)
	index := string(readContractFile(t, f.root, f.indexPath))
	index = strings.Replace(index, "> Shared model policy: none\n> Shared model reason: No shared wire model is changed in this batch.\n", "> Shared model policy: json-schema-v1\n", 1)
	index += "\n## Shared model baseline\n\n| Operation | Slot | Schema | Consumers | Valid example | Structural negative |\n|:--|:--|:--|:--|:--|:--|\n| create | order | [Order](../../architecture/data-model/order.schema.json) | [Backend](BE-001.md) | [Valid](../../architecture/data-model/order-valid.json) | [Invalid](../../architecture/data-model/order-invalid.json) |\n"
	writeContractFile(t, f.root, f.indexPath, index)
	contract := string(readContractFile(t, f.root, f.contractPath)) + "\n## Shared model inputs\n\n[Order](../../architecture/data-model/order.schema.json)\n"
	writeContractFile(t, f.root, f.contractPath, contract)
	schema := []byte(`{"type":"object","properties":{"id":{"type":"string"}},"required":["id"]}`)
	writeContractFile(t, f.root, "docs/architecture/data-model/order.schema.json", string(schema))
	writeContractFile(t, f.root, "docs/architecture/data-model/order-valid.json", `{"id":"order-1"}`)
	writeContractFile(t, f.root, "docs/architecture/data-model/order-invalid.json", `{}`)
	frozenInputs := f.packet.Inputs
	frozenInputs.Sources = []workpacket.Subject{{Path: "docs/architecture/data-model/order.schema.json", SHA256: workpacket.Digest(schema)}}
	packet, err := workpacket.BuildContracts(f.packet.Binding, frozenInputs)
	if err != nil {
		t.Fatal(err)
	}
	refs, err := runtime.ContractArtifacts(f.root, f.state, packet, f.answer, f.files)
	if err != nil {
		t.Fatal(err)
	}
	for _, ref := range refs {
		if ref.Path == "docs/architecture/data-model/order.schema.json" {
			t.Fatal("shared-model source already frozen upstream was duplicated as an S3 output ref")
		}
	}
	evidence, err := workpacket.ContractEvidence(packet, "E-S3-frozen-model", refs, f.answer, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	subjects := evidence["subject_refs"].([]workpacket.Subject)
	count := 0
	for _, subject := range subjects {
		if subject.Path == "docs/architecture/data-model/order.schema.json" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("frozen model source subject count = %d, want 1", count)
	}
}

func TestContractArtifactsScopesCoverageToCurrentAndDeclaredForeignREQBatches(t *testing.T) {
	f := newContractBatchFixture(t)
	foreignIndex := "# CONTRACTS-002\n> 需求: REQ-002\n> 状态: locked\n> 版本: v1.0.0\n\n## 合同清单\n\n| 合同ID | 类型 | 负责人 | 状态 | 版本 | 联调点 |\n|:--|:--|:--|:--|:--|:--|\n| FE-002 | 前端 | Builder | locked | v1.0.0 | N/A |\n"
	writeContractFile(t, f.root, "docs/dev/contracts/CONTRACTS-002.md", foreignIndex)
	writeContractFile(t, f.root, "docs/dev/contracts/FE-002.md", "# FE-002\n> 状态: locked\n> 版本: v1.0.0\n")
	refs, err := runtime.ContractArtifacts(f.root, f.state, f.packet, f.answer, f.files)
	if err != nil {
		t.Fatalf("foreign REQ batch was forced into current answer: %v", err)
	}
	if len(refs) != 2 || refs[0].Path != f.indexPath || refs[1].Path != f.contractPath {
		t.Fatalf("current answer included foreign batch: %#v", refs)
	}
	writeContractFile(t, f.root, "docs/dev/contracts/FE-003.md", "# Unowned draft\n> 状态: draft\n")
	if _, err := runtime.ContractArtifacts(f.root, f.state, f.packet, f.answer, f.files); err == nil || !strings.Contains(err.Error(), "unowned document") {
		t.Fatalf("unowned draft was allowed outside all REQ batches: %v", err)
	}
}

func TestContractArtifactsRejectsSecondIndexForBoundREQ(t *testing.T) {
	f := newContractBatchFixture(t)
	foreignIndex := "# CONTRACTS-002\n> 需求: REQ-001\n> 状态: locked\n> 版本: v1.0.0\n\n## 合同清单\n\n| 合同ID | 类型 | 负责人 | 状态 | 版本 | 联调点 |\n|:--|:--|:--|:--|:--|:--|\n| FE-002 | 前端 | Builder | locked | v1.0.0 | N/A |\n"
	writeContractFile(t, f.root, "docs/dev/contracts/CONTRACTS-002.md", foreignIndex)
	_, err := runtime.ContractArtifacts(f.root, f.state, f.packet, f.answer, f.files)
	if err == nil || !strings.Contains(err.Error(), "multiple contract indexes") {
		t.Fatalf("same-REQ second index was silently excluded: %v", err)
	}
}

func TestContractArtifactsRejectsMissingSharedModelApplicabilityDecision(t *testing.T) {
	f := newContractBatchFixture(t)
	index := string(readContractFile(t, f.root, f.indexPath))
	index = strings.Replace(index, "> Shared model policy: none\n", "", 1)
	index = strings.Replace(index, "> Shared model reason: No shared wire model is changed in this batch.\n", "", 1)
	writeContractFile(t, f.root, f.indexPath, index)
	_, err := runtime.ContractArtifacts(f.root, f.state, f.packet, f.answer, f.files)
	if err == nil || !strings.Contains(err.Error(), "explicit applicability decision") {
		t.Fatalf("missing shared-model applicability was accepted: %v", err)
	}
}

func TestContractArtifactsRejectsUnsubmittedContractScannedByOriginalCheck(t *testing.T) {
	f := newContractBatchFixture(t)
	draft := "# Contract\n> 状态：draft\n> 版本：v0.1.0\n"
	writeContractFile(t, f.root, "docs/dev/contracts/FE-002.md", draft)
	_, err := runtime.ContractArtifacts(f.root, f.state, f.packet, f.answer, f.files)
	if err == nil || !strings.Contains(err.Error(), "unowned document") {
		t.Fatalf("unsubmitted scanned contract error = %v", err)
	}
}

func TestContractArtifactsRejectsIndexContractSetContradiction(t *testing.T) {
	f := newContractBatchFixture(t)
	index := string(readContractFile(t, f.root, f.indexPath))
	index = strings.Replace(index, "| BE-001 | 后端 | Builder | locked | v1.0.0 | N/A |", "| BE-002 | 后端 | Builder | locked | v1.0.0 | N/A |", 1)
	writeContractFile(t, f.root, f.indexPath, index)
	_, err := runtime.ContractArtifacts(f.root, f.state, f.packet, f.answer, f.files)
	if err == nil || !strings.Contains(err.Error(), "index contract set differs") {
		t.Fatalf("index mismatch error = %v", err)
	}
	for _, owner := range []string{"REQ-002", "[REQ-001](../../requirements/REQ-002.md)", "REQ-001 / REQ-002"} {
		fresh := newContractBatchFixture(t)
		text := strings.Replace(string(readContractFile(t, fresh.root, fresh.indexPath)), "> 需求：REQ-001", "> 需求："+owner, 1)
		writeContractFile(t, fresh.root, fresh.indexPath, text)
		if _, err := runtime.ContractArtifacts(fresh.root, fresh.state, fresh.packet, fresh.answer, fresh.files); err == nil || !strings.Contains(err.Error(), "requirement declaration") {
			t.Fatalf("different/ambiguous requirement %q accepted: %v", owner, err)
		}
	}
	for _, cell := range []string{"[BE-001](BE-002.md)", ""} {
		fresh := newContractBatchFixture(t)
		text := strings.Replace(string(readContractFile(t, fresh.root, fresh.indexPath)), "| BE-001 |", "| "+cell+" |", 1)
		writeContractFile(t, fresh.root, fresh.indexPath, text)
		if _, err := runtime.ContractArtifacts(fresh.root, fresh.state, fresh.packet, fresh.answer, fresh.files); err == nil || !strings.Contains(err.Error(), "column 合同ID") {
			t.Fatalf("bad inventory cell %q missing specific feedback: %v", cell, err)
		}
	}

}

func TestContractArtifactsRejectsDraftAmongSubmittedContracts(t *testing.T) {
	f := newContractBatchFixture(t)
	content := string(readContractFile(t, f.root, f.contractPath))
	content = strings.Replace(content, "> 状态：locked", "> 状态：draft", 1)
	writeContractFile(t, f.root, f.contractPath, content)
	_, err := runtime.ContractArtifacts(f.root, f.state, f.packet, f.answer, f.files)
	if err == nil || !strings.Contains(err.Error(), "status=locked") {
		t.Fatalf("submitted draft error = %v", err)
	}
}

func TestContractArtifactsRejectsSourceViewCheckoutDrift(t *testing.T) {
	f := newContractBatchFixture(t)
	changed := append(readContractFile(t, f.root, f.contractPath), []byte("\nsource-view-only\n")...)
	view := replacingReader{Reader: f.files, path: f.contractPath, data: changed}
	_, err := runtime.ContractArtifacts(f.root, f.state, f.packet, f.answer, view)
	if err == nil || !strings.Contains(err.Error(), "checkout differs from source view") {
		t.Fatalf("source view mismatch error = %v", err)
	}
}

func TestContractArtifactsGitTreeOutputDriftExplainsBoundRef(t *testing.T) {
	f := newContractBatchFixture(t)
	view := commitContractBatchFixture(t, f)
	f.files = view
	updated := string(readContractFile(t, f.root, f.contractPath)) + "\ncommitted behavior fix\n"
	writeContractFile(t, f.root, f.contractPath, updated)

	_, err := runtime.ContractArtifacts(f.root, f.state, f.packet, f.answer, f.files)
	if err == nil {
		t.Fatal("uncommitted S3 contract output was accepted against its bound Git source")
	}
	for _, want := range []string{"checkout differs from source view", "refs/heads/feature/s3", "current S3 entry", "commit this file", "original S3 answer"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("S3 Git output diagnostic omitted %q: %v", want, err)
		}
	}
}

func TestContractArtifactsSharedModelGitTreeMismatchExplainsBoundRef(t *testing.T) {
	t.Run("changed checkout", func(t *testing.T) {
		f := newContractBatchFixture(t)
		prepareContractSharedModelBatch(t, f)
		view := commitContractBatchFixture(t, f)
		f.files = view
		writeContractFile(t, f.root, "docs/architecture/data-model/order.schema.json", `{"type":"object","properties":{"id":{"type":"integer"}},"required":["id"]}`)

		_, err := runtime.ContractArtifacts(f.root, f.state, f.packet, f.answer, f.files)
		assertS3GitOutputHint(t, err)
	})

	t.Run("missing from bound ref preserves closure error", func(t *testing.T) {
		f := newContractBatchFixture(t)
		prepareContractSharedModelBatch(t, f)
		if err := os.Remove(filepath.Join(f.root, "docs/architecture/data-model/order.schema.json")); err != nil {
			t.Fatal(err)
		}
		view := commitContractBatchFixture(t, f)
		f.files = view
		writeContractFile(t, f.root, "docs/architecture/data-model/order.schema.json", `{"type":"object","properties":{"id":{"type":"string"}},"required":["id"]}`)

		_, err := runtime.ContractArtifacts(f.root, f.state, f.packet, f.answer, f.files)
		if err == nil || !strings.Contains(err.Error(), "S3 shared-model source closure cannot be proven") {
			t.Fatalf("missing model source did not preserve the original closure error: %v", err)
		}
		if strings.Contains(err.Error(), "commit this file") {
			t.Fatalf("closure checker error received a path-inferred commit hint: %v", err)
		}
	})
}

type contractBatchFixture struct {
	root         string
	state        map[string]any
	packet       workpacket.ContractPacket
	answer       workpacket.ContractAnswer
	files        fileview.Reader
	indexPath    string
	contractPath string
}

func newContractBatchFixture(t *testing.T) contractBatchFixture {
	t.Helper()
	root := t.TempDir()
	indexPath := "docs/dev/contracts/CONTRACTS-001.md"
	contractPath := "docs/dev/contracts/BE-001.md"
	reqPath := "docs/requirements/REQ-001.md"
	designPath := "docs/architecture/ARCHITECTURE-001.md"
	req := "# Requirement\n> Status: locked\n"
	design := "# Architecture\n> 状态：locked\n> 版本：v1.0.0\n"
	index := "# Contract batch\n> 需求：REQ-001\n> 状态：locked\n> 版本：v1.0.0\n> Shared model policy: none\n> Shared model reason: No shared wire model is changed in this batch.\n\n" +
		"## 合同清单\n\n| 合同ID | 类型 | 负责人 | 状态 | 版本 | 联调点 |\n|:---|:---|:---|:---|:---|:---|\n| BE-001 | 后端 | Builder | locked | v1.0.0 | N/A |\n\n" +
		"## 联调点矩阵\n\n| 联调点 | 前端合同 | 后端合同 | 契约文档 | 状态 |\n|:---|:---|:---|:---|:---|\n\n" +
		"## 需求覆盖矩阵\n\n| REQ source_ref | Rule → CASE → Story → PATH → Spec | FE 合同条款 | BE 合同条款 | SYNC 条款 |\n|:---|:---|:---|:---|:---|\n"
	contract := "# Backend contract\n> 状态：locked\n> 版本：v1.0.0\n\n## Contract clauses\n\n| 本合同条款 | 条款 |\n|:---|:---|\n| §1 | Service behavior |\n"
	writeContractFile(t, root, reqPath, req)
	writeContractFile(t, root, designPath, design)
	writeContractFile(t, root, indexPath, index)
	writeContractFile(t, root, contractPath, contract)

	pass := "pass"
	binding := workpacket.PlanningBinding{RuntimeID: "runtime-contract", Generation: 1, StageEntry: 3, Stage: "S3", Owner: "Main", Requirement: workpacket.Reference{ID: "REQ-001", Path: reqPath, Version: "v1.0.0", SHA256: workpacket.Digest([]byte(req))}}
	designRef := workpacket.Reference{ID: "ARCHITECTURE-001", Path: designPath, Version: "v1.0.0", SHA256: workpacket.Digest([]byte(design))}
	packet, err := workpacket.BuildContracts(binding, workpacket.ContractInputs{Designs: []workpacket.Reference{designRef}})
	if err != nil {
		t.Fatal(err)
	}
	state := map[string]any{
		"schema_version": "1.2.0",
		"runtime_id":     binding.RuntimeID,
		"configuration":  map[string]any{"optimization": map[string]any{"packet": true}},
		"baseline":       map[string]any{"generation": 1},
		"lifecycle":      map[string]any{"state": "planning", "phase": "contracts", "phase_revision": 3},
		"bound_req": map[string]any{
			"id": "REQ-001", "path": reqPath, "version": "v1.0.0", "sha256": binding.Requirement.SHA256, "status": "locked",
			"workspace": map[string]any{"project_root": root},
		},
		"documents": []any{map[string]any{
			"kind": "design", "id": designRef.ID, "path": designRef.Path, "version": designRef.Version, "sha256": designRef.SHA256,
			"status": "locked", "generation": 1,
		}},
	}
	if err := os.MkdirAll(filepath.Join(root, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	stateBytes, _ := json.Marshal(state)
	if err := os.WriteFile(filepath.Join(root, ".claude", "loop-state.json"), stateBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	answer := workpacket.ContractAnswer{
		Conclusion: &pass, IndexPath: &indexPath, ContractPaths: []string{contractPath},
		Applicability: map[string]workpacket.ContractApplicability{
			"FE":   {Disposition: "not_applicable", Rationale: "The requirement has no client surface."},
			"BE":   {Disposition: "required", Rationale: "The requirement defines service behavior."},
			"SYNC": {Disposition: "not_applicable", Rationale: "No client/provider integration is required."},
		},
	}
	return contractBatchFixture{root: root, state: state, packet: packet, answer: answer, files: fileview.Disk{Root: root}, indexPath: indexPath, contractPath: contractPath}
}

type replacingReader struct {
	fileview.Reader
	path string
	data []byte
}

func (r replacingReader) ReadFile(path string) ([]byte, error) {
	if filepath.ToSlash(filepath.Clean(path)) == r.path {
		return append([]byte(nil), r.data...), nil
	}
	return r.Reader.ReadFile(path)
}

func writeContractFile(t *testing.T, root, path, content string) {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func prepareContractSharedModelBatch(t *testing.T, f contractBatchFixture) {
	t.Helper()
	index := string(readContractFile(t, f.root, f.indexPath))
	index = strings.Replace(index, "> Shared model policy: none\n> Shared model reason: No shared wire model is changed in this batch.\n", "> Shared model policy: json-schema-v1\n", 1)
	index += "\n## Shared model baseline\n\n| Operation | Slot | Schema | Consumers | Valid example | Structural negative |\n|:--|:--|:--|:--|:--|:--|\n| create | order | [Order](../../architecture/data-model/order.schema.json) | [Backend](BE-001.md) | [Valid](../../architecture/data-model/order-valid.json) | [Invalid](../../architecture/data-model/order-invalid.json) |\n"
	writeContractFile(t, f.root, f.indexPath, index)
	contract := string(readContractFile(t, f.root, f.contractPath)) + "\n## Shared model inputs\n\n[Order](../../architecture/data-model/order.schema.json)\n"
	writeContractFile(t, f.root, f.contractPath, contract)
	writeContractFile(t, f.root, "docs/architecture/data-model/order.schema.json", `{"type":"object","properties":{"id":{"type":"string"}},"required":["id"]}`)
	writeContractFile(t, f.root, "docs/architecture/data-model/order-valid.json", `{"id":"order-1"}`)
	writeContractFile(t, f.root, "docs/architecture/data-model/order-invalid.json", `{}`)
}

func commitContractBatchFixture(t *testing.T, f contractBatchFixture) *fileview.View {
	t.Helper()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", f.root}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %s %v", args, out, err)
		}
	}
	run("init", "-qb", "feature/s3")
	run("add", ".")
	run("-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-qm", "baseline")
	view, err := fileview.New(f.root, "refs/heads/feature/s3", []fileview.Rule{{Path: ".", Source: "git_tree"}})
	if err != nil {
		t.Fatal(err)
	}
	return view
}

func assertS3GitOutputHint(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("S3 model output mismatch was accepted")
	}
	for _, want := range []string{"refs/heads/feature/s3", "current S3 entry", "commit this file", "original S3 answer"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("S3 Git output diagnostic omitted %q: %v", want, err)
		}
	}
}

func readContractFile(t *testing.T, root, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestContractArtifactsGitIgnoresUnrelatedDirectoryMembers(t *testing.T) {
	f := newContractBatchFixture(t)
	f.files = commitContractBatchFixture(t, f)
	for _, path := range []string{"local-note.txt", "notes.md", ".BE-001.md.swp", "scratch/BE-999.md"} {
		writeContractFile(t, f.root, "docs/dev/contracts/"+path, "# Notes about BE-039\nLocal working notes.\n")
	}
	for name, fence := range map[string]string{"example-notes.md": "```", "tilde-notes.md": "~~~"} {
		writeContractFile(t, f.root, "docs/dev/contracts/"+name, "Local notes with no document title.\n\n"+fence+"markdown\n# BE-039 Example contract\n"+fence+"\n")
	}
	if _, err := runtime.ContractArtifacts(f.root, f.state, f.packet, f.answer, f.files); err != nil {
		t.Fatalf("unrelated checkout members blocked delivery: %v", err)
	}
	writeContractFile(t, f.root, "docs/dev/contracts/FE-002.md", "# FE-002\n> Status: draft\n")
	if _, err := runtime.ContractArtifacts(f.root, f.state, f.packet, f.answer, f.files); err == nil || !strings.Contains(err.Error(), "docs/dev/contracts/FE-002.md") {
		t.Fatalf("unsubmitted real contract candidate lacks its exact source diagnostic: %v", err)
	}
}

func TestContractArtifactsRejectsDescriptiveContractCandidate(t *testing.T) {
	for _, prefix := range []string{"", "```markdown\n# Example only\n```\n\n"} {
		f := newContractBatchFixture(t)
		writeContractFile(t, f.root, "docs/dev/contracts/service-contract.md", prefix+"# BE-999 Service contract\n> Status: draft\n")
		if _, err := runtime.ContractArtifacts(f.root, f.state, f.packet, f.answer, f.files); err == nil {
			t.Fatal("unowned contract identity with descriptive filename was accepted")
		}
	}
}

func TestContractArtifactsAcceptsCompleteBatchLargerThan64Contracts(t *testing.T) {
	f := newContractBatchFixture(t)
	index := string(readContractFile(t, f.root, f.indexPath))
	rows := ""
	for i := 2; i <= 96; i++ {
		id := fmt.Sprintf("BE-%03d", i)
		path := "docs/dev/contracts/" + id + ".md"
		rows += "| " + id + " | 后端 | Builder | locked | v1.0.0 | N/A |\n"
		writeContractFile(t, f.root, path, "# "+id+"\n> 状态：locked\n> 版本：v1.0.0\n")
		f.answer.ContractPaths = append(f.answer.ContractPaths, path)
	}
	index = strings.Replace(index, "| BE-001 | 后端 | Builder | locked | v1.0.0 | N/A |", rows+"| BE-001 | 后端 | Builder | locked | v1.0.0 | N/A |", 1)
	writeContractFile(t, f.root, f.indexPath, index)
	refs, err := runtime.ContractArtifacts(f.root, f.state, f.packet, f.answer, f.files)
	if err != nil || len(refs) != 97 {
		t.Fatalf("legal complete large batch rejected: size=%d err=%v", len(refs), err)
	}
}
