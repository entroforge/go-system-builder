package cli

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/repair"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

func TestS9RecoveryActionUsesFrozenNativeAuthorCoordinates(t *testing.T) {
	for _, scenario := range []string{"D", "legacy-C", "wrong-captured-D", "missing-pin", "unknown-version", "missing-D-plan"} {
		t.Run(scenario, func(t *testing.T) {
			root, worker := t.TempDir(), t.TempDir()
			p := workpacket.RepairPlanReportPacket{ID: "packet-" + strings.Repeat("a", 64), ViewPath: ".claude/work-packets/original/packet.json", AnswerPath: ".claude/submissions/original/answer.json"}
			_, pin, legacyResult := nativeS9Paths(p)
			plan, result, err := repair.NativeS9PrivatePaths(p.ID)
			if err != nil {
				t.Fatal(err)
			}
			pinBody := `{"base_commit":"original","private_submission_version":1}`
			if scenario == "legacy-C" {
				pinBody = `{"base_commit":"original","source_retention_version":1}`
				plan, result = p.AnswerPath, legacyResult
			}
			if scenario == "unknown-version" {
				pinBody = `{"base_commit":"original","private_submission_version":99}`
			}
			if scenario != "missing-pin" {
				writeRepairLeadFile(t, root, pin, pinBody)
			}
			writeRepairLeadFile(t, worker, ".git", "gitdir: original-source\n")
			writeRepairLeadFile(t, worker, result, `{"verdict":"completed"}`)
			if scenario != "missing-D-plan" {
				writeRepairLeadFile(t, worker, plan, `{"plan":"original professional draft"}`)
			}
			captured := filepath.Join(worker, result)
			if scenario == "wrong-captured-D" {
				// A readable old-looking file does not override the frozen D pin.
				captured = filepath.Join(worker, legacyResult)
				writeRepairLeadFile(t, worker, legacyResult, `{"verdict":"completed"}`)
			}
			handoff := s9HandoffProjection{
				Native:     true,
				Plan:       s9ListenerQuestionnaire{id: p.ID, view: p.ViewPath, answer: p.AnswerPath},
				Diagnostic: "original receiver could not finish its technical step",
				Attempt:    &nativeS9Attempt{Kind: "Builder", InputRef: captured, FailureKind: "system"},
			}
			message := s9UnreceivedActionContext(context.Background(), root, handoff)
			positive := scenario == "D" || scenario == "legacy-C"
			if positive {
				if !strings.Contains(message, s9OriginalConsumeCommand(root, captured)) {
					t.Fatal(message)
				}
			} else if strings.Contains(message, " runtime stage-answer consume ") {
				t.Fatalf("unproven source gained recovery command: %s", message)
			}
			if scenario == "missing-D-plan" && !strings.Contains(message, filepath.Join(worker, plan)) {
				t.Fatal(message)
			}
			if scenario == "missing-pin" || scenario == "unknown-version" {
				for _, want := range []string{"Original native author contract is unavailable", filepath.Join(root, pin), "no private author path or version was inferred"} {
					if !strings.Contains(message, want) {
						t.Fatalf("missing %q: %s", want, message)
					}
				}
				for _, guessed := range []string{plan, result, legacyResult} {
					if strings.Contains(message, guessed) {
						t.Fatalf("guessed private coordinate %q: %s", guessed, message)
					}
				}
			}
		})
	}
}
