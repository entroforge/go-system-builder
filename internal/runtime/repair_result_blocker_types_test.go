package runtime

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/schema"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

func TestRepairResultBlockerArtifactCanonicalizesFullAnswerAndRejectsUnknowns(t *testing.T) {
	artifact := repairResultBlockerArtifactFixture(t)
	pretty, err := json.MarshalIndent(artifact, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	var got RepairResultBlockerArtifact
	if err := json.Unmarshal(pretty, &got); err != nil {
		t.Fatalf("unmarshal blocker artifact: %v", err)
	}
	wantAnswer, err := canonicalRuntimeJSON(artifact.SubmittedAnswer)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got.SubmittedAnswer, wantAnswer) {
		t.Fatalf("submitted answer did not canonicalize:\n got %s\nwant %s", got.SubmittedAnswer, wantAnswer)
	}
	if got.PacketID != artifact.PacketID || got.Binding.Sources.OwnerAgentID != artifact.Binding.Sources.OwnerAgentID {
		t.Fatalf("blocker artifact identity changed during round trip: %#v", got)
	}

	t.Run("unknown artifact field", func(t *testing.T) {
		data := repairResultBlockerJSONWithField(t, artifact, "accepted_sequence", json.RawMessage(`1`))
		var decoded RepairResultBlockerArtifact
		if err := json.Unmarshal(data, &decoded); err == nil {
			t.Fatal("blocker artifact accepted receipt-only acceptance coordinates")
		}
	})

	t.Run("incomplete null answer field", func(t *testing.T) {
		data, err := json.Marshal(artifact)
		if err != nil {
			t.Fatal(err)
		}
		var object map[string]json.RawMessage
		if err := json.Unmarshal(data, &object); err != nil {
			t.Fatal(err)
		}
		var answer map[string]json.RawMessage
		if err := json.Unmarshal(object["submitted_answer"], &answer); err != nil {
			t.Fatal(err)
		}
		answer["migration_ref"] = json.RawMessage(`null`)
		object["submitted_answer"], err = json.Marshal(answer)
		if err != nil {
			t.Fatal(err)
		}
		data, err = json.Marshal(object)
		if err != nil {
			t.Fatal(err)
		}
		var decoded RepairResultBlockerArtifact
		if err := json.Unmarshal(data, &decoded); err == nil {
			t.Fatal("blocker artifact accepted a partial answer")
		}
	})

	t.Run("unknown nested binding field", func(t *testing.T) {
		data, err := json.Marshal(artifact)
		if err != nil {
			t.Fatal(err)
		}
		var object map[string]json.RawMessage
		if err := json.Unmarshal(data, &object); err != nil {
			t.Fatal(err)
		}
		var binding map[string]json.RawMessage
		if err := json.Unmarshal(object["binding"], &binding); err != nil {
			t.Fatal(err)
		}
		binding["unrecognized_authority"] = json.RawMessage(`true`)
		object["binding"], err = json.Marshal(binding)
		if err != nil {
			t.Fatal(err)
		}
		data, err = json.Marshal(object)
		if err != nil {
			t.Fatal(err)
		}
		var decoded RepairResultBlockerArtifact
		if err := json.Unmarshal(data, &decoded); err == nil {
			t.Fatal("blocker artifact accepted unknown binding authority")
		}
	})

	t.Run("unknown full-answer field", func(t *testing.T) {
		data, err := json.Marshal(artifact)
		if err != nil {
			t.Fatal(err)
		}
		var object map[string]json.RawMessage
		if err := json.Unmarshal(data, &object); err != nil {
			t.Fatal(err)
		}
		var answer map[string]json.RawMessage
		if err := json.Unmarshal(object["submitted_answer"], &answer); err != nil {
			t.Fatal(err)
		}
		answer["forged_result_ref"] = json.RawMessage(`"result.json"`)
		object["submitted_answer"], err = json.Marshal(answer)
		if err != nil {
			t.Fatal(err)
		}
		object["answer_sha256"], err = json.Marshal(workpacket.Digest(mustCanonicalBlockerAnswer(t, object["submitted_answer"])))
		if err != nil {
			t.Fatal(err)
		}
		data, err = json.Marshal(object)
		if err != nil {
			t.Fatal(err)
		}
		var decoded RepairResultBlockerArtifact
		if err := json.Unmarshal(data, &decoded); err == nil {
			t.Fatal("blocker artifact accepted an unknown answer field")
		}
	})

	t.Run("duplicate nested answer key", func(t *testing.T) {
		data, err := json.Marshal(artifact)
		if err != nil {
			t.Fatal(err)
		}
		var object map[string]json.RawMessage
		if err := json.Unmarshal(data, &object); err != nil {
			t.Fatal(err)
		}
		object["submitted_answer"] = json.RawMessage(strings.Replace(
			string(artifact.SubmittedAnswer),
			`"verdict":"blocked"`,
			`"verdict":"blocked","verdict":"completed"`,
			1,
		))
		data, err = json.Marshal(object)
		if err != nil {
			t.Fatal(err)
		}
		var decoded RepairResultBlockerArtifact
		if err := json.Unmarshal(data, &decoded); err == nil {
			t.Fatal("blocker artifact accepted duplicate answer keys")
		}
	})
}

func TestRepairResultBlockerReceiptIsReadOnlyProjection(t *testing.T) {
	receipt := RepairResultBlockerReceipt{
		Contract:                     "s9-repair-result-blocker-receipt-v1",
		ArtifactRef:                  ".claude/evidence/loop-demo/g2/S9/blockers/packet-" + strings.Repeat("a", 64) + "/" + strings.Repeat("b", 64) + ".json",
		ArtifactSHA256:               strings.Repeat("b", 64),
		Artifact:                     repairResultBlockerArtifactFixture(t),
		AcceptedEventID:              "evt-agent-agent-demo-work_blocked-r12",
		AcceptedIdempotencyKey:       "runtime:agent:agent-demo:work_blocked:11",
		AcceptedAfterRuntimeRevision: 12,
		AcceptedSequence:             37,
	}
	wire, err := json.MarshalIndent(receipt, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	var decoded RepairResultBlockerReceipt
	if err := json.Unmarshal(wire, &decoded); err != nil {
		t.Fatalf("unmarshal blocker receipt: %v", err)
	}
	if decoded.AcceptedEventID != receipt.AcceptedEventID || decoded.AcceptedSequence != receipt.AcceptedSequence {
		t.Fatalf("read-only receipt coordinates changed: %#v", decoded)
	}
	if !bytes.Equal(decoded.Artifact.SubmittedAnswer, mustCanonicalBlockerAnswer(t, receipt.Artifact.SubmittedAnswer)) {
		t.Fatal("receipt did not retain the canonical blocker answer")
	}

	var object map[string]json.RawMessage
	if err := json.Unmarshal(wire, &object); err != nil {
		t.Fatal(err)
	}
	object["write_capability"] = json.RawMessage(`true`)
	unknown, err := json.Marshal(object)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(unknown, &RepairResultBlockerReceipt{}); err == nil {
		t.Fatal("blocker receipt accepted an unknown capability field")
	}
	compact, err := json.Marshal(receipt)
	if err != nil {
		t.Fatal(err)
	}
	duplicate := strings.Replace(string(compact), `"accepted_sequence":37`, `"accepted_sequence":37,"accepted_sequence":38`, 1)
	if duplicate == string(compact) {
		t.Fatal("test setup did not locate receipt sequence")
	}
	if err := json.Unmarshal([]byte(duplicate), &RepairResultBlockerReceipt{}); err == nil {
		t.Fatal("blocker receipt accepted duplicate acceptance coordinates")
	}
	t.Run("missing artifact", func(t *testing.T) {
		var value map[string]json.RawMessage
		if err := json.Unmarshal(wire, &value); err != nil {
			t.Fatal(err)
		}
		delete(value, "artifact")
		data, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(data, &RepairResultBlockerReceipt{}); err == nil {
			t.Fatal("blocker receipt accepted coordinates without its artifact")
		}
	})
}

func TestRepairResultBlockerArtifactBindsCanonicalAnswerSHA(t *testing.T) {
	artifact := repairResultBlockerArtifactFixture(t)
	data := repairResultBlockerJSONWithField(t, artifact, "answer_sha256", json.RawMessage(`"`+strings.Repeat("0", 64)+`"`))
	if err := json.Unmarshal(data, &RepairResultBlockerArtifact{}); err == nil || !strings.Contains(err.Error(), "canonical answer SHA256") {
		t.Fatalf("expected canonical answer digest mismatch, got %v", err)
	}
}

func TestRepairResultBlockerSchemaAcceptsBothExecutionSlotSpellings(t *testing.T) {
	artifact := repairResultBlockerArtifactFixture(t)
	for _, slot := range []string{artifact.Binding.Sources.ManifestAssignmentID, artifact.Binding.Sources.RepairAssignmentID} {
		t.Run(slot, func(t *testing.T) {
			copy := artifact
			copy.ExecutionAssignmentID = slot
			data, err := json.Marshal(copy)
			if err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(data, &RepairResultBlockerArtifact{}); err != nil {
				t.Fatalf("blocker DTO rejected an original execution ID spelling: %v", err)
			}
		})
	}
}

func TestRepairResultBlockerSchemaRequiresCompleteNonpassAnswer(t *testing.T) {
	artifact := repairResultBlockerArtifactFixture(t)
	for _, verdict := range []string{"blocked", "contract_conflict", "stale", "completed"} {
		t.Run(verdict, func(t *testing.T) {
			copy := artifact
			copy.SubmittedAnswer = json.RawMessage(strings.Replace(string(copy.SubmittedAnswer), `"verdict":"blocked"`, `"verdict":"`+verdict+`"`, 1))
			copy.AnswerSHA256 = workpacket.Digest(mustCanonicalBlockerAnswer(t, copy.SubmittedAnswer))
			data, err := json.Marshal(copy)
			if err != nil {
				t.Fatal(err)
			}
			err = schema.NewEmbeddedValidator().ValidateBytes("repair-result-blocker.schema.json", data)
			if (err == nil) != (verdict != "completed") {
				t.Fatalf("blocker schema verdict=%s: %v", verdict, err)
			}
		})
	}
	for _, name := range []string{"verdict", "before_fix_results", "unit_results", "detection_gap_artifacts", "evidence_refs", "scope_deviations", "residual_risks", "migration_ref", "rollback_ref"} {
		t.Run("null "+name, func(t *testing.T) {
			copy := artifact
			var answer map[string]json.RawMessage
			if err := json.Unmarshal(copy.SubmittedAnswer, &answer); err != nil {
				t.Fatal(err)
			}
			answer[name] = json.RawMessage(`null`)
			var err error
			copy.SubmittedAnswer, err = json.Marshal(answer)
			if err != nil {
				t.Fatal(err)
			}
			data, err := json.Marshal(copy)
			if err != nil {
				t.Fatal(err)
			}
			if err := schema.NewEmbeddedValidator().ValidateBytes("repair-result-blocker.schema.json", data); err == nil {
				t.Fatalf("standalone blocker schema accepted null %s", name)
			}
		})
	}
}

func repairResultBlockerArtifactFixture(t *testing.T) RepairResultBlockerArtifact {
	t.Helper()
	sha := strings.Repeat("a", 64)
	commit := strings.Repeat("b", 40)
	answer := json.RawMessage(`{"verdict":"blocked","before_fix_results":[{"assertion_id":"root-1","observed_red":"failure reproduced","evidence_refs":["evidence://red"]}],"unit_results":[{"unit_id":"unit-api","status":"blocked","evidence_refs":["evidence://unit"],"implementation_summary":"work is blocked","restored_invariant_refs":[]}],"detection_gap_artifacts":[],"evidence_refs":["evidence://red"],"scope_deviations":[],"residual_risks":[],"migration_ref":"","rollback_ref":""}`)
	return RepairResultBlockerArtifact{
		Contract:        "s9-repair-result-blocker-v1",
		SchemaVersion:   "1.0.0",
		PacketID:        "packet-" + sha,
		PacketSHA256:    sha,
		IssuanceEventID: "evt-repair-result-packet-packet-" + sha,
		Binding: workpacket.RepairResultBinding{
			Sources: workpacket.RepairPlanReportBinding{
				RuntimeID: "loop-demo", BaselineGeneration: 2, ReviewRound: 1,
				CaseID: "investigation-case-demo", CaseRef: "case.json", CaseSHA256: sha,
				ContractID: "repair-contract-demo", ContractRef: "contract.json", ContractSHA256: sha,
				SessionID: "repair-session-demo", SessionRef: "session.json", SessionSHA256: sha,
				PlanID: "repair-plan-demo", PlanRef: "plan.json", PlanSHA256: sha,
				RepairAssignmentID: "repair-assignment-demo", RepairAssignmentSHA256: sha,
				ManifestAssignmentID: "assignment-demo", ManifestAssignmentSHA256: sha,
				TeamID: "team-demo", TaskID: "TASK-demo", TaskRef: "TASK-demo.md", TaskSHA256: sha,
				ManifestRef: "manifest.json", ManifestSHA256: sha, OwnerAgentID: "agent-demo",
			},
			PlanReportRef: "report.json", PlanReportSHA256: sha, ExecutionSHA256: sha,
		},
		ExecutionAssignmentID: "assignment-demo",
		ExecutionGeneration:   3,
		ExecutionRoot:         "/work/assignment-demo",
		PlatformSessionID:     "platform-session-demo",
		BaseCommit:            commit,
		SourceHead:            commit,
		AnswerPath:            ".claude/submissions/loop-demo/g2/agent-demo/answer.json",
		SubmittedAnswer:       answer,
		AnswerSHA256:          workpacket.Digest(mustCanonicalBlockerAnswer(t, answer)),
		MessageRef:            ".claude/evidence/loop-demo/g2/S9/blocker-messages/packet-" + sha + "/" + sha + ".json",
		MessageSHA256:         sha,
	}
}

func repairResultBlockerJSONWithField(t *testing.T, artifact RepairResultBlockerArtifact, name string, value json.RawMessage) []byte {
	t.Helper()
	data, err := json.Marshal(artifact)
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil {
		t.Fatal(err)
	}
	object[name] = value
	data, err = json.Marshal(object)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func mustCanonicalBlockerAnswer(t *testing.T, answer []byte) []byte {
	t.Helper()
	canonical, err := canonicalRuntimeJSON(answer)
	if err != nil {
		t.Fatal(err)
	}
	return canonical
}
