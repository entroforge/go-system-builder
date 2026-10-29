package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/cli"
	"github.com/entroforge/go-system-builder/internal/semantic"
	"github.com/entroforge/go-system-builder/internal/submission"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	fixtures "github.com/entroforge/go-system-builder/tests/fixtures/req039"
)

func designGuidanceHook(t *testing.T, root, event, tool, path string) string {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"hook_event_name": event, "session_id": "design-recovery", "cwd": root, "tool_name": tool, "tool_input": map[string]any{"file_path": filepath.Join(root, path)}})
	var out, diagnostic bytes.Buffer
	if code := cli.Run([]string{"hook", "--root", root, "--event", event}, bytes.NewReader(body), &out, &diagnostic); code != 0 {
		t.Fatalf("%s: %d %s %s", event, code, &out, &diagnostic)
	}
	return out.String()
}

func TestS2PendingOutletRetainsActualGapAcrossObservation(t *testing.T) {
	root, p := newDesignFixture(t, "none")
	arch := "docs/architecture/ARCHITECTURE-039.md"
	writeContractSource(t, root, arch, "# ARCHITECTURE-039\n> Status: draft\n> Version: v1.0.0\n")
	stageJSON(t, root, p.AnswerPath, designDraft(p))
	// The actual S2 save listener uses disk design authority, without a Git
	// FileView; S3 preparation independently enforces committed sources.
	service := &submission.Service{Root: root, StatePath: filepath.Join(root, ".claude/loop-state.json"), JournalPath: filepath.Join(root, ".claude/loop-events.jsonl"), Validator: semantic.RuntimeCandidateValidator{}}
	receipt, err := service.SaveDesign(context.Background(), p.AnswerPath)
	if err != nil || !receipt.Accepted || receipt.WorkComplete || !strings.Contains(receipt.NextAction, "document:design:locked") {
		t.Fatalf("missing original lock gap: %+v %v", receipt, err)
	}
	revision := stageSnapshot(t, root).State["revision"]
	for _, event := range []string{"PreToolUse", "SessionStart", "Stop"} {
		output := designGuidanceHook(t, root, event, "Read", arch)
		if !strings.Contains(output, "Last outlet attempt") || !strings.Contains(output, "document:design:locked") {
			t.Fatalf("%s lost the actual outlet failure: %s", event, output)
		}
	}
	if stageSnapshot(t, root).State["revision"] != revision {
		t.Fatal("observation mutated Runtime")
	}
	if err := os.Remove(filepath.Join(root, p.AnswerPath)); err != nil {
		t.Fatal(err)
	}
	output := designGuidanceHook(t, root, "PreToolUse", "Read", arch)
	if !strings.Contains(output, p.AnswerPath) || !strings.Contains(output, "Restore its original authored bytes") {
		t.Fatalf("accepted missing answer was hidden: %s", output)
	}
	designGuidanceHook(t, root, "PostToolUse", "Write", arch)
	if _, err := os.Stat(filepath.Join(root, p.AnswerPath)); !os.IsNotExist(err) {
		t.Fatalf("accepted answer recreated: %v", err)
	}
	stageJSON(t, root, p.AnswerPath, designDraft(p))
	writeContractSource(t, root, arch, "# ARCHITECTURE-039\n> Status: locked\n> Version: v1.0.0\n")
	before := stageSnapshot(t, root)
	if receipt, err = service.SaveDesign(context.Background(), p.AnswerPath); err != nil || !receipt.WorkComplete {
		t.Fatalf("original outlet did not resume: %+v %v", receipt, err)
	}
	if diagnostic := submission.PlanningDiagnostic(root, before.State, "outlet"); diagnostic != "" {
		t.Fatalf("successful outlet retained failure: %s", diagnostic)
	}
}

func TestS2MissingQuestionnaireRestoresOriginalFilesOnMaterialChange(t *testing.T) {
	root, p := newDesignFixture(t, "none")
	stageJSON(t, root, p.AnswerPath, designDraft(p))
	authored, err := os.ReadFile(filepath.Join(root, p.AnswerPath))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, p.ViewPath)); err != nil {
		t.Fatal(err)
	}
	output := designGuidanceHook(t, root, "PreToolUse", "Read", "docs/architecture/ARCHITECTURE-039.md")
	if !strings.Contains(output, "registered files above are missing") || !strings.Contains(output, "runtime stage-answer issue") {
		t.Fatalf("missing recovery direction: %s", output)
	}
	before := stageSnapshot(t, root)
	designGuidanceHook(t, root, "PostToolUse", "Write", "docs/architecture/ARCHITECTURE-039.md")
	if _, err := os.Stat(filepath.Join(root, p.ViewPath)); err != nil {
		t.Fatal(err)
	}
	if current, err := os.ReadFile(filepath.Join(root, p.AnswerPath)); err != nil || !bytes.Equal(current, authored) {
		t.Fatal("view restoration overwrote authored answer")
	}
	if stageSnapshot(t, root).State["revision"] != before.State["revision"] {
		t.Fatal("file restoration replaced original issuance")
	}
	if err := os.Remove(filepath.Join(root, p.AnswerPath)); err != nil {
		t.Fatal(err)
	}
	designGuidanceHook(t, root, "PostToolUse", "Write", "docs/architecture/ARCHITECTURE-039.md")
	if _, err := os.Stat(filepath.Join(root, p.AnswerPath)); err != nil {
		t.Fatal(err)
	}
}

func TestS3PreparationFailureSurvivesReadAndClearsAfterActualRepair(t *testing.T) {
	root, p := newDesignFixture(t, "none")
	stageJSON(t, root, p.AnswerPath, designDraft(p))
	service, err := submission.New(root)
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := service.SaveDesign(context.Background(), p.AnswerPath)
	if err != nil || !receipt.WorkComplete || !strings.Contains(receipt.NextAction, "preparation failed") {
		t.Fatalf("expected separate preparation failure: %+v %v", receipt, err)
	}
	before := stageSnapshot(t, root)
	diagnostic := submission.PlanningDiagnostic(root, before.State, "preparation")
	if diagnostic == "" {
		t.Fatal("lost preparation diagnostic")
	}
	for _, event := range []string{"PreToolUse", "SessionStart"} {
		output := designGuidanceHook(t, root, event, "Read", "docs/project-map.md")
		// Hook JSON escapes punctuation; the material path and diagnostic marker
		// must survive without invoking full source validation again.
		if !strings.Contains(output, "Last preparation attempt") || !strings.Contains(output, "docs/design") {
			t.Fatalf("lost actual failure: %s", output)
		}
	}
	if stageSnapshot(t, root).State["revision"] != before.State["revision"] {
		t.Fatal("observation advanced Runtime")
	}
	designGuidanceHook(t, root, "PreToolUse", "Bash", "")
	writeContractSource(t, root, "docs/design/README.md", "# Design source manifest\n")
	fixtures.CommitCurrentFixture(t, root)
	post := designGuidanceHook(t, root, "PostToolUse", "Bash", "")
	if !strings.Contains(post, "/S3/") || !strings.Contains(post, "additionalContext") {
		t.Fatalf("preparation paths were not delivered in the repair event: %s", post)
	}
	if diagnostic := submission.PlanningDiagnostic(root, before.State, "preparation"); diagnostic != "" {
		t.Fatalf("successful preparation retained diagnostic: %s", diagnostic)
	}
	output := designGuidanceHook(t, root, "PreToolUse", "Read", "docs/project-map.md")
	if strings.Contains(output, "Last preparation attempt") || !strings.Contains(output, "/S3/") {
		t.Fatalf("repaired preparation was not projected: %s", output)
	}
}

func TestS2RecoveryKeepsPreviouslyIssuedTemplate(t *testing.T) {
	root, current := newDesignFixture(t, "none")
	// Model a prior release with its own template digest and packet identity.
	prior := current
	prior.Draft.Instruction = "Earlier issued design instructions."
	prior.TemplateSHA = workpacket.Digest([]byte(prior.Draft.Instruction))
	identity, _ := json.Marshal(struct {
		Binding     workpacket.PlanningBinding
		TemplateSHA string
	}{prior.Binding, prior.TemplateSHA})
	prior.ID = "packet-" + workpacket.Digest(identity)
	prior.ViewPath = strings.ReplaceAll(current.ViewPath, current.ID, prior.ID)
	prior.AnswerPath = strings.ReplaceAll(current.AnswerPath, current.ID, prior.ID)
	data, _ := json.Marshal(prior)
	var row map[string]any
	if err := json.Unmarshal(data, &row); err != nil {
		t.Fatal(err)
	}
	state := stageSnapshot(t, root).State
	state["entities"].(map[string]any)["work_packets"] = []any{row}
	fixtures.WriteState(t, root, state)
	stageJSON(t, root, prior.AnswerPath, designDraft(prior))
	service, err := submission.New(root)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := service.IssueDesign(context.Background())
	if err != nil || restored.ID != prior.ID || restored.Draft.Instruction != prior.Draft.Instruction {
		t.Fatalf("recovery rebuilt a new template: %+v %v", restored, err)
	}
	data, err = os.ReadFile(filepath.Join(root, prior.ViewPath))
	if err != nil || !bytes.Contains(data, []byte(prior.Draft.Instruction)) {
		t.Fatalf("original view not restored: %s %v", data, err)
	}
	if len(stageSnapshot(t, root).State["entities"].(map[string]any)["work_packets"].([]any)) != 1 {
		t.Fatal("recovery issued a second responsibility")
	}
}

func TestS2CLIRecoverUsesSameDiskAuthorityAsOriginalSave(t *testing.T) {
	root, p := newDesignFixture(t, "none")
	arch := "docs/architecture/ARCHITECTURE-039.md"
	writeContractSource(t, root, arch, "# ARCHITECTURE-039\n> Status: draft\n> Version: v1.0.0\n")
	stageJSON(t, root, p.AnswerPath, designDraft(p))
	designGuidanceHook(t, root, "PostToolUse", "Write", p.AnswerPath)
	if stageSnapshot(t, root).State["lifecycle"].(map[string]any)["phase"] != "design" {
		t.Fatal("draft architecture advanced original save")
	}
	// Git still contains the old locked architecture. Technical recovery must
	// use the same current disk artifact as the original S2 save listener.
	stageCLI(t, "runtime", "stage-answer", "recover", "--root", root, "--agent", "Main")
	if stageSnapshot(t, root).State["lifecycle"].(map[string]any)["phase"] != "design" {
		t.Fatal("CLI recovery borrowed locked status from Git")
	}
	fixtures.CommitCurrentFixture(t, root)
	writeContractSource(t, root, arch, "# ARCHITECTURE-039\n> Status: locked\n> Version: v1.0.0\n")
	// Now Git is draft and disk is locked. The S2 outlet can complete; S3
	// preparation independently reports the actual uncommitted-source gap.
	before := stageSnapshot(t, root)
	stageCLI(t, "runtime", "stage-answer", "recover", "--root", root, "--agent", "Main")
	after := stageSnapshot(t, root)
	if after.State["lifecycle"].(map[string]any)["phase"] != "contracts" {
		t.Fatal("CLI recovery ignored repaired disk architecture")
	}
	if len(after.State["evidence"].([]any)) != len(before.State["evidence"].([]any)) {
		t.Fatal("technical recovery repeated professional reception")
	}
	if diagnostic := submission.PlanningDiagnostic(root, before.State, "outlet"); diagnostic != "" {
		t.Fatal(diagnostic)
	}
	if diagnostic := submission.PlanningDiagnostic(root, after.State, "preparation"); !strings.Contains(diagnostic, arch) {
		t.Fatalf("S3 source failure lost: %s", diagnostic)
	}
}
