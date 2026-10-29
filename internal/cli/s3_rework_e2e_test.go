package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/cli"
	"github.com/entroforge/go-system-builder/internal/fileview"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	fixtures "github.com/entroforge/go-system-builder/tests/fixtures/req039"
)

// This fixture enters S3 through the accepted S2 design, then deliberately
// leaves docs/dev/contracts absent while Main commits the actual upstream sources. The
// S3 packet and all later facts are produced by the installed listener,
// original consumer, and original PTR-PLAN-02 route.
func newS3ReworkFixture(t *testing.T) string {
	t.Helper()
	root, design := newDesignFixture(t, "none")
	stageJSON(t, root, design.AnswerPath, designDraft(design))
	stageCLI(t, "runtime", "stage-answer", "consume", "--root", root, "--agent", "Main", "--path", design.AnswerPath)
	settleMainPlanningFollowups(t, root)
	writeContractSource(t, root, "docs/design/README.md", "# Design sources\n")
	for _, rel := range []string{"docs/dev/tasks/TASK-template.md", "docs/rules/dispatch-plan.md"} {
		data, err := os.ReadFile(filepath.Join(fixtures.RepoRoot(t), rel))
		if err != nil {
			t.Fatal(err)
		}
		writeContractSource(t, root, rel, string(data))
	}
	if _, err := os.Stat(filepath.Join(root, "docs/dev/contracts")); !os.IsNotExist(err) {
		t.Fatalf("cold S3 fixture must start without a contract directory: err=%v", err)
	}
	return root
}

func s3PacketFromCurrentRuntime(t *testing.T, root string) workpacket.ContractPacket {
	t.Helper()
	snapshot := stageSnapshot(t, root)
	entities, _ := snapshot.State["entities"].(map[string]any)
	rows, _ := entities["work_packets"].([]any)
	var packetID string
	for _, raw := range rows {
		row, _ := raw.(map[string]any)
		if row["template_id"] != "s3-contract-v1" {
			continue
		}
		if packetID != "" {
			t.Fatal("more than one S3 packet was issued for the current stage entry")
		}
		packetID, _ = row["packet_id"].(string)
	}
	if packetID == "" {
		t.Fatal("the original source commit outlet did not prepare its S3 questionnaire")
	}
	packet, found, err := runtime.FindContractPacket(snapshot.State, packetID)
	if err != nil || !found {
		t.Fatalf("load listener-issued S3 questionnaire: found=%t err=%v", found, err)
	}
	return packet
}

func prepareS3ThroughSourceCommit(t *testing.T, root string) workpacket.ContractPacket {
	t.Helper()
	// The actual authored upstream sources become available on this commit.
	// A script save is observed through its matching Pre/Post pair; command
	// text in an isolated Post is not proof that a material change occurred.
	tool := map[string]any{"hook_event_name": "PreToolUse", "session_id": "s3-source-commit", "tool_use_id": "source-commit", "cwd": root, "tool_name": "Bash", "tool_input": map[string]any{"command": "git commit -qm 'delivered fixture'"}}
	before, _ := json.Marshal(tool)
	var beforeOut, beforeErr bytes.Buffer
	if code := cli.Run([]string{"hook", "--root", root, "--event", "PreToolUse"}, bytes.NewReader(before), &beforeOut, &beforeErr); code != 0 {
		t.Fatalf("source commit Pre: %d %s %s", code, &beforeOut, &beforeErr)
	}
	fixtures.CommitCurrentFixture(t, root)
	tool["hook_event_name"] = "PostToolUse"
	tool["tool_response"] = map[string]any{"exit_code": 0}
	changed, _ := json.Marshal(tool)
	var changedOut, changedErr bytes.Buffer
	if code := cli.Run([]string{"hook", "--root", root, "--event", "PostToolUse"}, bytes.NewReader(changed), &changedOut, &changedErr); code != 0 {
		t.Fatalf("actual S3 source commit observation: %d %s %s", code, &changedOut, &changedErr)
	}
	beforeRead := stageSnapshot(t, root)

	body, err := json.Marshal(map[string]any{
		"hook_event_name": "PreToolUse",
		"tool_name":       "Read",
		"cwd":             root,
	})
	if err != nil {
		t.Fatal(err)
	}
	var out, errout bytes.Buffer
	if code := cli.Run([]string{"hook", "--root", root, "--event", "PreToolUse"}, bytes.NewReader(body), &out, &errout); code != 0 {
		t.Fatalf("cold S3 Pre: code=%d stdout=%s stderr=%s", code, out.String(), errout.String())
	}
	var envelope struct {
		Output struct {
			Context    string `json:"additionalContext"`
			Permission string `json:"permissionDecision"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
		t.Fatalf("decode S3 listener guidance: %v; output=%s", err, out.String())
	}
	if envelope.Output.Permission == "deny" {
		t.Fatalf("S3 progress listener denied a tool: %s", out.String())
	}
	if stageSnapshot(t, root).Revision != beforeRead.Revision {
		t.Fatal("ordinary S3 Read issued or changed responsibility")
	}
	packet := s3PacketFromCurrentRuntime(t, root)
	var delivered struct {
		Output struct {
			Context string `json:"additionalContext"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal(changedOut.Bytes(), &delivered); err != nil {
		t.Fatalf("source commit did not deliver a model response: %v %s", err, &changedOut)
	}
	for _, path := range []string{packet.ViewPath, packet.AnswerPath} {
		if !strings.Contains(delivered.Output.Context, filepath.Join(root, filepath.FromSlash(path))) {
			t.Fatalf("source commit did not deliver next S3 path %s: %s", path, &changedOut)
		}
		if !strings.Contains(envelope.Output.Context, filepath.Join(root, filepath.FromSlash(path))) {
			t.Fatalf("Pre did not give exact S3 path %s: %s", path, envelope.Output.Context)
		}
	}
	for _, source := range packet.Inputs.Sources {
		if strings.HasPrefix(source.Path, "docs/dev/contracts/") || strings.HasPrefix(source.Path, "docs/architecture/data-model/") {
			t.Fatalf("S3's not-yet-authored delivery was frozen as an issuance input: %+v", source)
		}
	}
	return packet
}

func s3AnswerDraft(packet workpacket.ContractPacket, indexPath string, contractPaths ...string) workpacket.ContractDraft {
	draft := packet.Draft
	conclusion := "pass"
	draft.Answer = workpacket.ContractAnswer{
		Conclusion:    &conclusion,
		IndexPath:     &indexPath,
		ContractPaths: append([]string(nil), contractPaths...),
		Applicability: map[string]workpacket.ContractApplicability{
			"FE":   {Disposition: "not_applicable", Rationale: "The bound requirement has no client surface."},
			"BE":   {Disposition: "required", Rationale: "The service boundary owns this shared request model."},
			"SYNC": {Disposition: "not_applicable", Rationale: "No separate client/provider protocol is required for this fixture."},
		},
	}
	return draft
}

func writeS3SharedModelBatch(t *testing.T, root string, includeForeignBatch, includeUnknownDraft bool) (indexPath, contractPath string, modelPaths []string) {
	t.Helper()
	indexPath = "docs/dev/contracts/CONTRACTS-039.md"
	contractPath = "docs/dev/contracts/BE-039.md"
	index := `# REQ-039 service contracts
> 需求：REQ-039
> 状态：locked
> 版本：v1.0.0
> Shared model policy: json-schema-v1

## 合同清单

| 合同ID | 类型 | 负责人 | 状态 | 版本 | 联调点 |
|:---|:---|:---|:---|:---|:---|
| BE-039 | 后端 | Main | locked | v1.0.0 | N/A |

## Shared model baseline

| Operation | Slot | Schema | Consumers | Valid example | Structural negative |
|:--|:--|:--|:--|:--|:--|
| createOrder | request | [Order request](../../architecture/data-model/order.schema.json) | [BE contract](BE-039.md) | [valid](../../architecture/data-model/order-valid.json) | [structural negative](../../architecture/data-model/order-invalid.json) |

## 需求覆盖矩阵

| REQ source_ref | FE 合同条款 | BE 合同条款 | SYNC 条款 |
|:---|:---|:---|:---|
| REQ-039/NFR-001 | — | BE-039 §1 | — |
`
	contract := `# BE-039 service responsibility
> 状态：locked
> 版本：v1.0.0

## Shared model inputs

- [Order request](../../architecture/data-model/order.schema.json)

## Contract clauses

| 本合同条款 | 条款 |
|:---|:---|
| §1 | Validate the shared request shape before service effects. |
`
	writeContractSource(t, root, indexPath, index)
	writeContractSource(t, root, contractPath, contract)

	modelFiles := map[string]string{
		"docs/architecture/data-model/order.schema.json":         `{"$schema":"https://json-schema.org/draft/2020-12/schema","type":"object","properties":{"order":{"$ref":"order-details.schema.json"}},"required":["order"],"additionalProperties":false}`,
		"docs/architecture/data-model/order-details.schema.json": `{"$schema":"https://json-schema.org/draft/2020-12/schema","type":"object","properties":{"id":{"type":"string"}},"required":["id"],"additionalProperties":false}`,
		"docs/architecture/data-model/order-valid.json":          `{"order":{"id":"order-1"}}`,
		"docs/architecture/data-model/order-invalid.json":        `{"order":{"id":17}}`,
	}
	for path, content := range modelFiles {
		writeContractSource(t, root, path, content+"\n")
		modelPaths = append(modelPaths, path)
	}

	if includeForeignBatch {
		writeContractSource(t, root, "docs/dev/contracts/CONTRACTS-038.md", `# Historical REQ-038 contracts
> 需求：REQ-038
> 状态：locked
> 版本：v1.0.0
> Shared model policy: none
> Shared model reason: This independent historical batch has no shared wire model.

## 合同清单

| 合同ID | 类型 | 负责人 | 状态 | 版本 | 联调点 |
|:---|:---|:---|:---|:---|:---|
| BE-038 | 后端 | Main | locked | v1.0.0 | N/A |

## 需求覆盖矩阵

| REQ source_ref | FE 合同条款 | BE 合同条款 | SYNC 条款 |
|:---|:---|:---|:---|
`)
		writeContractSource(t, root, "docs/dev/contracts/BE-038.md", `# Historical backend contract
> 状态：locked
> 版本：v1.0.0

## Contract clauses

| 本合同条款 | 条款 |
|:---|:---|
| §1 | Independent historical service behavior. |
`)
	}
	if includeUnknownDraft {
		writeContractSource(t, root, "docs/dev/contracts/FE-039.md", `# Unlisted FE draft
> 状态：draft
> 版本：v0.1.0

## Contract clauses

An unlisted current-REQ draft must not enter the accepted batch.
`)
	}
	return indexPath, contractPath, modelPaths
}

func saveS3ThroughPostToolUse(t *testing.T, root, answerPath string) (string, string) {
	t.Helper()
	body, err := json.Marshal(map[string]any{
		"hook_event_name": "PostToolUse",
		"tool_name":       "Write",
		"cwd":             root,
		"tool_input":      map[string]any{"file_path": filepath.Join(root, filepath.FromSlash(answerPath))},
	})
	if err != nil {
		t.Fatal(err)
	}
	var out, errout bytes.Buffer
	if code := cli.Run([]string{"hook", "--root", root, "--event", "PostToolUse"}, bytes.NewReader(body), &out, &errout); code != 0 {
		t.Fatalf("S3 answer PostToolUse: code=%d stdout=%s stderr=%s", code, out.String(), errout.String())
	}
	return out.String(), errout.String()
}

func TestS3ColdListenerSaveRegistersCurrentBatchAndSharedModelClosure(t *testing.T) {
	root := newS3ReworkFixture(t)
	packet := prepareS3ThroughSourceCommit(t, root)
	indexPath, contractPath, modelPaths := writeS3SharedModelBatch(t, root, true, false)
	// Start from the actual published author file. Main supplies professional
	// content only; source hashes, receipt identity and time come from consumers.
	data, err := os.ReadFile(filepath.Join(root, packet.AnswerPath))
	if err != nil {
		t.Fatal(err)
	}
	var draft workpacket.ContractDraft
	if err := json.Unmarshal(data, &draft); err != nil {
		t.Fatal(err)
	}
	if draft.Answer.Conclusion != nil || draft.Answer.IndexPath != nil || len(draft.Answer.ContractPaths) != 0 || len(draft.Answer.Applicability) != 0 {
		t.Fatal("published author file prefilled professional judgments")
	}
	draft.Answer = s3AnswerDraft(packet, indexPath, contractPath).Answer
	stageJSON(t, root, packet.AnswerPath, draft)
	// The project source contract is git_tree. The original consumer therefore
	// evaluates the authored, complete S3 delivery from the committed branch.
	fixtures.CommitCurrentFixture(t, root)

	output, stderr := saveS3ThroughPostToolUse(t, root, packet.AnswerPath)
	if !strings.Contains(output, "S3 SUBMISSION RECEIPT") {
		t.Fatalf("original S3 consumer did not return a receipt: stdout=%s stderr=%s", output, stderr)
	}
	snapshot := stageSnapshot(t, root)
	record, found, err := runtime.FindSubmission(snapshot.State, packet.ID)
	if err != nil || !found || record.ControlStatus != "advanced" {
		t.Fatalf("original S3 consumer/PTR result: submission=%+v found=%t err=%v output=%s stderr=%s", record, found, err, output, stderr)
	}
	lifecycle, _ := snapshot.State["lifecycle"].(map[string]any)
	if lifecycle["state"] != "planning" || lifecycle["phase"] != "tasks" {
		t.Fatalf("original PTR-PLAN-02 did not advance to S4: %+v", lifecycle)
	}

	registered := map[string]bool{}
	for _, raw := range snapshot.State["documents"].([]any) {
		row, _ := raw.(map[string]any)
		if row["kind"] == "contract" {
			registered[stringValueForS3Test(row["path"])] = true
		}
	}
	if !registered[indexPath] || !registered[contractPath] || registered["docs/dev/contracts/CONTRACTS-038.md"] || registered["docs/dev/contracts/BE-038.md"] {
		t.Fatalf("PTR did not register only the current bound REQ contract batch: %v", registered)
	}

	evidencePath := ""
	for _, raw := range snapshot.State["evidence"].([]any) {
		row, _ := raw.(map[string]any)
		if row["id"] == record.EvidenceID {
			evidencePath = stringValueForS3Test(row["path"])
			break
		}
	}
	if evidencePath == "" {
		t.Fatalf("original S3 evidence record %s is missing", record.EvidenceID)
	}
	data, err = os.ReadFile(filepath.Join(root, filepath.FromSlash(evidencePath)))
	if err != nil {
		t.Fatal(err)
	}
	var evidence struct {
		SubjectRefs []workpacket.Subject      `json:"subject_refs"`
		Answer      workpacket.ContractAnswer `json:"contract_answer"`
	}
	if err := json.Unmarshal(data, &evidence); err != nil {
		t.Fatal(err)
	}
	subjects := map[string]workpacket.Subject{}
	for _, subject := range evidence.SubjectRefs {
		subjects[subject.Path] = subject
	}
	for _, path := range append([]string{indexPath, contractPath}, modelPaths...) {
		subject, ok := subjects[path]
		if !ok {
			t.Errorf("original planning_contract evidence omitted delivered subject %s: %+v", path, evidence.SubjectRefs)
			continue
		}
		content, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
		if err != nil || subject.SHA256 != workpacket.Digest(content) {
			t.Errorf("subject %s is not pinned to its exact submitted bytes: subject=%+v err=%v", path, subject, err)
		}
	}
	if evidence.Answer.IndexPath == nil || *evidence.Answer.IndexPath != indexPath || len(evidence.Answer.ContractPaths) != 1 || evidence.Answer.ContractPaths[0] != contractPath {
		t.Fatalf("original consumer did not preserve the one-REQ professional answer: %+v", evidence.Answer)
	}
	// S3's exit must be readable by the original next-stage source adapter.
	// This only inspects the handoff; it does not perform S4 authoring.
	inputs, err := runtime.ResolveTaskPlanningInputs(root, snapshot.State, fileview.Disk{Root: root})
	if err != nil {
		t.Fatalf("accepted S3 delivery is not consumable at its original exit: %v", err)
	}
	for _, path := range modelPaths {
		found := false
		for _, source := range inputs.Sources {
			if source.Path == path && source.SHA256 == subjects[path].SHA256 {
				found = true
			}
		}
		if !found {
			t.Errorf("S3 exit lost its accepted model subject %s", path)
		}
	}
}

func TestS3ColdListenerRejectsUnknownCurrentREQDraft(t *testing.T) {
	root := newS3ReworkFixture(t)
	packet := prepareS3ThroughSourceCommit(t, root)
	indexPath, contractPath, _ := writeS3SharedModelBatch(t, root, false, true)
	stageJSON(t, root, packet.AnswerPath, s3AnswerDraft(packet, indexPath, contractPath))
	fixtures.CommitCurrentFixture(t, root)
	before := stageSnapshot(t, root)

	output, _ := saveS3ThroughPostToolUse(t, root, packet.AnswerPath)
	after := stageSnapshot(t, root)
	if _, found, err := runtime.FindSubmission(after.State, packet.ID); err != nil || found {
		t.Fatalf("unlisted current-REQ draft was accepted: found=%t err=%v output=%s", found, err, output)
	}
	if before.Revision != after.Revision || len(before.State["evidence"].([]any)) != len(after.State["evidence"].([]any)) {
		t.Fatalf("rejected S3 draft changed original Runtime/evidence: before=%d after=%d", before.Revision, after.Revision)
	}
	lifecycle, _ := after.State["lifecycle"].(map[string]any)
	if lifecycle["state"] != "planning" || lifecycle["phase"] != "contracts" {
		t.Fatalf("unlisted draft advanced the S3 cursor: %+v", lifecycle)
	}
}

func stringValueForS3Test(value any) string {
	text, _ := value.(string)
	return text
}
