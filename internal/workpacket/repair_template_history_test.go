package workpacket

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/schema"
)

// A changed instruction changes packet identity. Old issued questions remain
// reconstructible from their exact original embedded bytes, never from a draft.
func TestS9NaturalObservationPreservesFrozenTemplates(t *testing.T) {
	t.Run("PlanReport", func(t *testing.T) {
		const oldSHA = "04750203e18d01207a33aa3f84880f8451d4ff9b89446eddd27cef3ace0a0110"
		data, err := schema.ReadAsset("stage-form-s9-plan-template-before-natural-observation.json")
		if err != nil || Digest(data) != oldSHA {
			t.Fatalf("original Plan template bytes changed: %v", err)
		}
		binding, inputs := repairPlanReportTestFixture(t)
		old, err := buildRepairPlanReportWithTemplate(binding, inputs, data)
		if err != nil {
			t.Fatal(err)
		}
		current, err := BuildRepairPlanReport(binding, inputs)
		if err != nil {
			t.Fatal(err)
		}
		frozen, _ := json.Marshal(old)
		var saved RepairPlanReportPacket
		if err := json.Unmarshal(frozen, &saved); err != nil {
			t.Fatal(err)
		}
		rebuilt, err := RebuildRepairPlanReport(saved)
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := json.Marshal(rebuilt)
		if !bytes.Equal(raw, frozen) {
			t.Fatal("historical Plan packet, paths or fields changed")
		}
		if _, err := RepairPlanReportPacketDigest(saved); err != nil {
			t.Fatal(err)
		}
		if current.ID == old.ID || current.ReportID == old.ReportID || current.IssuanceEventID == old.IssuanceEventID || current.TemplateSHA == old.TemplateSHA {
			t.Fatal("new Plan reused old issuance identity")
		}
		if !reflect.DeepEqual(current.Binding, old.Binding) || !reflect.DeepEqual(current.Inputs, old.Inputs) || !reflect.DeepEqual(current.Draft.Questions, old.Draft.Questions) || !reflect.DeepEqual(current.Draft.Read, old.Draft.Read) || !bytes.Equal(current.Draft.Answer, old.Draft.Answer) {
			t.Fatal("Plan professional fields or sources changed")
		}
		assertS9NaturalInstruction(t, current.Draft.Instruction)
		if !strings.Contains(old.Draft.Instruction, "普通保存只保留工作") {
			t.Fatal("old Plan instruction changed")
		}
		answer, _ := json.Marshal(map[string]any{"answer": map[string]any{
			"plan":           "Verify the original boundary before the scoped repair.",
			"red_checks":     []any{map[string]any{"name": "boundary", "command": "go test ./internal/api", "result": "pass", "evidence_refs": []string{"test://precheck"}}},
			"proposed_paths": []string{"internal/api/payload.go"},
		}})
		if _, fields := ValidateRepairPlanReportAnswer(old, answer); len(fields) != 0 {
			t.Fatalf("old Plan answer rejected: %#v", fields)
		}
		if _, fields := ValidateRepairPlanReportAnswer(current, answer); len(fields) != 0 {
			t.Fatalf("same professional Plan rejected by new template: %#v", fields)
		}
		old.Draft.Instruction = current.Draft.Instruction
		if _, err := RepairPlanReportPacketDigest(old); err == nil {
			t.Fatal("tampered old Plan accepted")
		}
		old.TemplateSHA = strings.Repeat("f", 64)
		if _, err := RebuildRepairPlanReport(old); err == nil {
			t.Fatal("unknown Plan template accepted")
		}
	})
	t.Run("RepairResult", func(t *testing.T) {
		const oldSHA = "56cd501c2d0ff973139cf3618785eda0c3a77386fbb750826721ec1fc52e40fe"
		data, err := schema.ReadAsset("stage-form-s9-result-template-before-natural-observation.json")
		if err != nil || Digest(data) != oldSHA {
			t.Fatalf("original Result template bytes changed: %v", err)
		}
		binding, inputs := repairResultTestFixture(t)
		old, err := buildRepairResultWithTemplate(binding, inputs, data)
		if err != nil {
			t.Fatal(err)
		}
		current, err := BuildRepairResult(binding, inputs)
		if err != nil {
			t.Fatal(err)
		}
		frozen, _ := json.Marshal(old)
		var saved RepairResultPacket
		if err := json.Unmarshal(frozen, &saved); err != nil {
			t.Fatal(err)
		}
		rebuilt, err := RebuildRepairResult(saved)
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := json.Marshal(rebuilt)
		if !bytes.Equal(raw, frozen) {
			t.Fatal("historical Result packet, paths or fields changed")
		}
		if _, err := RepairResultPacketDigest(saved); err != nil {
			t.Fatal(err)
		}
		if current.ID == old.ID || current.ResultID == old.ResultID || current.IssuanceEventID == old.IssuanceEventID || current.TemplateSHA == old.TemplateSHA {
			t.Fatal("new Result reused old issuance identity")
		}
		if !reflect.DeepEqual(current.Binding, old.Binding) || !reflect.DeepEqual(current.Inputs, old.Inputs) || !reflect.DeepEqual(current.Draft.Questions, old.Draft.Questions) || !reflect.DeepEqual(current.Draft.Read, old.Draft.Read) || !bytes.Equal(current.Draft.Answer, old.Draft.Answer) {
			t.Fatal("Result professional fields or sources changed")
		}
		assertS9NaturalInstruction(t, current.Draft.Instruction)
		if !strings.Contains(old.Draft.Instruction, "普通保存只保留工作") {
			t.Fatal("old Result instruction changed")
		}
		answer := repairResultFilledAnswerFile(t, old, repairResultCompleteAnswer())
		if _, fields := ValidateRepairResultAnswer(old, answer); len(fields) != 0 {
			t.Fatalf("old answer rejected: %#v", fields)
		}
		if _, fields := ValidateRepairResultAnswer(current, answer); len(fields) != 0 {
			t.Fatalf("same professional answer rejected by new template: %#v", fields)
		}
		old.Draft.Instruction = current.Draft.Instruction
		if _, err := RepairResultPacketDigest(old); err == nil {
			t.Fatal("tampered old Result accepted")
		}
		old.TemplateSHA = strings.Repeat("f", 64)
		if _, err := RebuildRepairResult(old); err == nil {
			t.Fatal("unknown Result template accepted")
		}
	})
}

func assertS9NaturalInstruction(t *testing.T, instruction string) {
	t.Helper()
	for _, required := range []string{"PreToolUse", "PostToolUse", "Stop／SubagentStop", "返回路径不是额外接收前提"} {
		if !strings.Contains(instruction, required) {
			t.Fatalf("new instruction omitted %q: %s", required, instruction)
		}
	}
	if strings.Contains(instruction, "普通保存只保留工作") {
		t.Fatal("new instruction retained the conflicting reception rule")
	}
}
