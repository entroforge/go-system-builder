package transition_test

import (
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/transition"
)

func TestManualHeaderContainsNoVolatileTimestamp(t *testing.T) {
	manual := transition.RenderManual(&transition.LoopDefinition{}, transition.ManualOptions{
		TargetPath:           "loop-harness.md",
		HarnessVersion:       "dev",
		LoopDefinitionSHA256: strings.Repeat("a", 64),
	})
	if strings.Contains(manual, "**Generated**") {
		t.Fatalf("manual output must be deterministic; got volatile timestamp header: %s", manual)
	}
}

func TestManualEvidenceBindingGuidanceIncludesSlotTemplateAndAcceptedKinds(t *testing.T) {
	manual := transition.RenderManual(&transition.LoopDefinition{
		Transitions: []transition.TransitionSpec{
			{
				ID:               "TR-006",
				From:             "building",
				To:               "verification",
				RequiredEvidence: []string{"builder_report_record", "team_manifest_record"},
				Description:      "Start a review round.",
			},
		},
	}, transition.ManualOptions{})

	for _, want := range []string{
		"--evidence builder_report_record=<reference>",
		"--evidence team_manifest_record=<reference>",
		"Accepted kinds: `builder_report`, `agent_completion`",
		"Accepted kinds: `builder_report`, `team_manifest`",
	} {
		if !strings.Contains(manual, want) {
			t.Fatalf("manual must contain %q, got:\n%s", want, manual)
		}
	}
}

func TestManualIncludesExplicitS11HumanDecisionGuidance(t *testing.T) {
	manual := transition.RenderManual(&transition.LoopDefinition{}, transition.ManualOptions{})

	for _, want := range []string{
		"runtime human-decision",
		"approve",
		"defer",
		"reject_defect",
		"reject_acceptance",
		"reject_release_audit",
		"abort",
		"release_authorized",
		"Harness has no squash merge, publication, deployment, or formal release permission",
	} {
		if !strings.Contains(manual, want) {
			t.Fatalf("S11 manual guidance must contain %q, got:\n%s", want, manual)
		}
	}
}

func TestManualS7RecoveryGuidanceNamesRepairFactsAndTypedEvidence(t *testing.T) {
	manual := transition.RenderManual(&transition.LoopDefinition{}, transition.ManualOptions{})

	for _, want := range []string{
		"coverage_inventory",
		"e2e_assets",
		"capture step --finding <id> --claim <id>",
		"explicitly named legacy/recovery command rejects an artifact",
		"runtime s7-budget-decision",
	} {
		if !strings.Contains(manual, want) {
			t.Fatalf("S7 manual guidance must contain %q, got:\n%s", want, manual)
		}
	}
}

func TestManualNativeS6AndQuestionnaireS7S9Flow(t *testing.T) {
	manual := transition.RenderManual(&transition.LoopDefinition{}, transition.ManualOptions{})

	for _, want := range []string{
		"docs/examples/s6-builder/README.md",
		"s6 prepare --root <authority-root> --task <TASK> --role <Builder-role>",
		"SubagentStop invokes the original Result consumer",
		"Ordinary save hooks do not receive native S6 delivery",
		"Main explicitly runs `runtime task-integrate --assignment-id <id>`",
		"s6 prepare --revise-assignment <id> --revision-intent-key <stable-key>",
		"exact Hook-supplied ReviewPlan path",
		"Do not run `runtime review-result submit`",
		"independent Targeted verification per approved Contract",
		"no extra Handoff report",
	} {
		if !strings.Contains(manual, want) {
			t.Fatalf("normal questionnaire path must contain %q, got:\n%s", want, manual)
		}
	}

	for _, stale := range []string{
		"Use the returned begin/commit/report/check commands",
		"team launch --manifest <path> --request-template <template>",
		"runtime review-plan --file <plan.json>",
		"send the generic PLAN_REPORT and submit the S9 domain PlanReport",
	} {
		if strings.Contains(manual, stale) {
			t.Fatalf("manual still advertises a duplicate normal-path step %q", stale)
		}
	}
}

func TestManualS10GuidanceNamesManifestAndClosedLoop(t *testing.T) {
	manual := transition.RenderManual(&transition.LoopDefinition{}, transition.ManualOptions{})

	for _, want := range []string{
		"actual acceptance or release-audit Manifest at the supplied exact path",
		"Explain requirement dispositions, applicable observations, counterevidence",
		"Eight audit themes are relevance prompts, not a row quota",
		"original consumer derives its envelope and metrics",
		"do not run a second validate/render/evidence submission",
		"Unknowns and blockers remain unfinished",
		"Actual human choice remains in S11",
		"runtime human-decision",
	} {
		if !strings.Contains(manual, want) {
			t.Fatalf("S10 manual guidance must contain %q, got:\n%s", want, manual)
		}
	}
	for _, stale := range []string{"audit_manifest_path", "audit_manifest_sha256", "loop-harness s10 manifest validate", "must include all eight", "exactly one counterevidence row per item"} {
		if strings.Contains(manual, stale) {
			t.Fatalf("normal S10 guidance must not ask the author to copy or submit %q", stale)
		}
	}
}

func TestManualApprovedAmendmentUsesOriginalReceiverAndCurrentQuestionnaire(t *testing.T) {
	def := &transition.LoopDefinition{Transitions: []transition.TransitionSpec{{ID: "TR-020", From: "paused", To: "planning", RequiredEvidence: []string{"human_decision_record", "req_lock_record"}}}}
	manual := transition.RenderManual(def, transition.ManualOptions{})
	for _, want := range []string{
		"## Approved requirement amendment {#approved-requirement-amendment}",
		"skip this step when already paused",
		"runtime pause --root <project-root> --approved-by <actual-human> --reason <approved-correction>",
		"same REQ ID", "raise its semantic version", "bound development branch",
		"req amend --root <project-root> --req docs/requirements/REQ-<id>.md --approved-by <actual-human>",
		"exact current S2 questionnaire view/answer paths", "do not ask for the same approval again",
	} {
		if !strings.Contains(manual, want) {
			t.Fatalf("normal amendment guidance missing %q", want)
		}
	}
	legacy := transition.RenderTransition(def, "TR-020")
	if !strings.Contains(legacy, "Low-level diagnostic evidence slots only") || !strings.Contains(legacy, "#approved-requirement-amendment") || strings.Contains(legacy, "copy into `runtime transition`") || strings.Contains(legacy, "retry with the command above") {
		t.Fatalf("TR020 still presented as normal evidence composition: %s", legacy)
	}
	if err := transition.ValidateManualEvidenceBindings(def, manual); err != nil {
		t.Fatal(err)
	}
}

func TestManualCleanRoundUsesOriginalCompletionAndNamedRecovery(t *testing.T) {
	manual := transition.RenderManual(&transition.LoopDefinition{Transitions: []transition.TransitionSpec{{ID: "TR-009", From: "verification", To: "acceptance", RequiredEvidence: []string{"clean_round_record"}}}}, transition.ManualOptions{})
	for _, want := range []string{"final accepted ReviewResult", "runtime review-continue", "runtime audit-prepare", "material recovery does not repeat TR-009"} {
		if !strings.Contains(manual, want) {
			t.Fatalf("clean-round handoff omitted %q", want)
		}
	}
	section := manual[strings.Index(manual, "### `TR-009`"):]
	if strings.Contains(section, "Evidence bindings (copy into") || strings.Contains(section, "If a binding is missing, retry") {
		t.Fatal("clean-round handoff asks author to copy machine bindings")
	}
}
