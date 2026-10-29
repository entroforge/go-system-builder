package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/fileview"
	"github.com/entroforge/go-system-builder/internal/submission"
	fixtures "github.com/entroforge/go-system-builder/tests/fixtures/req039"
)

func TestS2ScopeGuidanceExposesBatchBeforeSubmission(t *testing.T) {
	root, _ := newDesignFixture(t, "none")
	writeContractSource(t, root, "docs/architecture/ARCHITECTURE-039-alternative.md", "# Alternative\n> Status: draft\n> REQ: REQ-039\n")
	writeContractSource(t, root, "docs/architecture/ARCHITECTURE-other.md", "# Other\n> Status: draft\n> REQ: REQ-040\n")
	for _, module := range []string{"current", "unrelated", "template", "templates"} {
		writeContractSource(t, root, "docs/design/prototypes/"+module+"/note.md", "# Module\n")
	}
	before, err := os.ReadFile(filepath.Join(root, ".claude/loop-state.json"))
	if err != nil {
		t.Fatal(err)
	}
	out := designGuidanceEvent(t, root, "scope", "PreToolUse", "Read")
	var envelope struct {
		Specific struct {
			Context string `json:"additionalContext"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal([]byte(out), &envelope); err != nil {
		t.Fatalf("invalid Hook response: %v %s", err, out)
	}
	context := envelope.Specific.Context
	for _, required := range []string{
		"ARCHITECTURE-039-alternative.md — status=draft; outlet gap",
		"ARCHITECTURE-039.md — status=locked",
		"unlabelled-document compatibility",
		filepath.Join(root, "docs/design/prototypes/current"),
		filepath.Join(root, "docs/design/prototypes/unrelated"),
		"Global source consumption does not ask you to redesign unrelated modules",
	} {
		if !strings.Contains(context, required) {
			t.Fatalf("missing pre-submission scope %q: %s", required, context)
		}
	}
	for _, excluded := range []string{"ARCHITECTURE-other.md", "\n- " + filepath.Join(root, "docs/design/prototypes/template")} {
		if strings.Contains(context, excluded) {
			t.Fatalf("incorrect batch/module member %q: %s", excluded, context)
		}
	}
	if repeated := designGuidanceEvent(t, root, "scope", "PreToolUse", "Read"); !strings.Contains(repeated, "guidance is unchanged") {
		t.Fatalf("unchanged inventory was repeated: %s", repeated)
	}
	writeContractSource(t, root, "docs/architecture/ARCHITECTURE-039-alternative.md", "# Adopted separate design\n> Status: locked\n> REQ: REQ-039\n")
	changed := designGuidanceEvent(t, root, "scope", "PreToolUse", "Read")
	if strings.Contains(changed, "guidance is unchanged") || strings.Contains(changed, "status=draft; outlet gap") || !strings.Contains(changed, "ARCHITECTURE-039-alternative.md") {
		t.Fatalf("updated membership/status not delivered: %s", changed)
	}
	after, err := os.ReadFile(filepath.Join(root, ".claude/loop-state.json"))
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("scope observation wrote Runtime")
	}
}

func TestS2PendingBatchRecoveryKeepsBoundSource(t *testing.T) {
	root, packet := newDesignFixture(t, "none")
	writeContractSource(t, root, "docs/architecture/ARCHITECTURE-039-alternative.md", "# Alternative\n> Status: draft\n> REQ: REQ-039\n")
	stageJSON(t, root, packet.AnswerPath, designDraft(packet))
	service, err := submission.New(root)
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := service.SaveDesign(context.Background(), packet.AnswerPath)
	if err != nil || !receipt.Accepted || receipt.WorkComplete {
		t.Fatalf("expected received report with pending batch: %+v %v", receipt, err)
	}
	ref, err := fileview.DevelopmentRef(stageSnapshot(t, root).State)
	if err != nil {
		t.Fatal(err)
	}
	out := designGuidanceEvent(t, root, "resumed-batch", "SessionStart", "Read")
	for _, required := range []string{"already recorded", "ARCHITECTURE-039-alternative.md", "bound development source " + ref, "do not refill its professional answer"} {
		if !strings.Contains(out, required) {
			t.Fatalf("resumed pending outlet lost %q: %s", required, out)
		}
	}
	if strings.Contains(out, "actual self-review conclusion is unfinished") {
		t.Fatalf("recovery asked for already received professional work: %s", out)
	}
}

func TestS2LargeGlobalModuleSetPreparesS3(t *testing.T) {
	root, packet := newDesignFixture(t, "none")
	for i := 0; i < 129; i++ {
		writeContractSource(t, root, fmt.Sprintf("docs/design/prototypes/unrelated/note-%03d.md", i), "# Existing module note\n")
	}
	fixtures.CommitCurrentFixture(t, root)
	stageJSON(t, root, packet.AnswerPath, designDraft(packet))
	service, err := submission.New(root)
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := service.SaveDesign(context.Background(), packet.AnswerPath)
	if err != nil || !receipt.Accepted || !receipt.WorkComplete || receipt.CurrentStage != "S3" {
		t.Fatalf("original outlet failed: %+v %v", receipt, err)
	}
	if strings.Contains(receipt.NextAction, "preparation failed") {
		t.Fatalf("global source size blocked next responsibility: %s", receipt.NextAction)
	}
	snapshot := stageSnapshot(t, root)
	prepared := false
	for _, raw := range snapshot.State["entities"].(map[string]any)["work_packets"].([]any) {
		row := raw.(map[string]any)
		if row["template_id"] != "s3-contract-v1" {
			continue
		}
		prepared = true
		for _, field := range []string{"view_path", "answer_path"} {
			if _, err := os.Stat(filepath.Join(root, row[field].(string))); err != nil {
				t.Fatalf("next responsibility path missing: %v", err)
			}
		}
	}
	if !prepared {
		t.Fatal("S3 responsibility not issued despite committed global sources")
	}
}
