package cli_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/submission"
	fixtures "github.com/entroforge/go-system-builder/tests/fixtures/req039"
)

func TestS2AlternateIssuedAnswerReceivesFromOriginalPostHook(t *testing.T) {
	root, packet := newDesignFixture(t, "none", func(root string) {
		if err := os.MkdirAll(filepath.Join(root, ".claude"), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, ".claude/submissions"), []byte("unknown owner"), 0600); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.HasPrefix(packet.AnswerPath, ".claude/submissions-alt-") {
		t.Fatalf("no actual alternate answer path: %s", packet.AnswerPath)
	}
	writeContractSource(t, root, "docs/design/README.md", "# Design sources\n")
	fixtures.CommitCurrentFixture(t, root)
	out, _ := planningBoundaryEvent(t, root, "PreToolUse", "Write", packet.AnswerPath, false)
	if strings.Contains(out, `"permissionDecision":"deny"`) {
		t.Fatalf("original Main tool denied: %s", out)
	}
	stageJSON(t, root, packet.AnswerPath, designDraft(packet))
	out, diagnostic := planningBoundaryEvent(t, root, "PostToolUse", "Write", packet.AnswerPath, false)
	snap := stageSnapshot(t, root)
	record, found, err := runtime.FindSubmission(snap.State, packet.ID)
	if err != nil || !found || record.ControlStatus != "advanced" {
		t.Fatalf("actual alternate answer was not received: %+v %v stdout=%s stderr=%s", record, err, out, diagnostic)
	}
	if life := snap.State["lifecycle"].(map[string]any); life["phase"] != "contracts" {
		t.Fatalf("S3 not reached: %+v", life)
	}
	if data, err := os.ReadFile(filepath.Join(root, ".claude/submissions")); err != nil || string(data) != "unknown owner" {
		t.Fatalf("unknown original root changed: %q %v", data, err)
	}
}

func TestS2RecordedPendingContinuesOnMaterialSaveWithoutPrivateAnswer(t *testing.T) {
	root, packet := newDesignFixture(t, "none")
	writeContractSource(t, root, "docs/design/README.md", "# Design sources\n")
	fixtures.CommitCurrentFixture(t, root)
	stageJSON(t, root, packet.AnswerPath, designDraft(packet))
	service, err := submission.New(root)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ctx = &cancelAfterDurableReception{Context: ctx, statePath: service.StatePath, packetID: packet.ID, cancel: cancel}
	receipt, err := service.SaveDesign(ctx, packet.AnswerPath)
	if err != nil || !receipt.Accepted || receipt.WorkComplete {
		t.Fatalf("original pending reception: %+v %v", receipt, err)
	}
	before := stageSnapshot(t, root)
	prior, found, err := runtime.FindSubmission(before.State, packet.ID)
	if err != nil || !found || prior.ControlStatus != "pending" {
		t.Fatalf("missing original pending: %+v %v", prior, err)
	}
	if err := os.Remove(filepath.Join(root, packet.AnswerPath)); err != nil {
		t.Fatal(err)
	}
	material := "docs/architecture/ARCHITECTURE-039.md"
	planningBoundaryEvent(t, root, "PreToolUse", "Write", material, false)
	path := filepath.Join(root, material)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(data, []byte("\nProfessional clarification.\n")...), 0600); err != nil {
		t.Fatal(err)
	}
	out, diagnostic := planningBoundaryEvent(t, root, "PostToolUse", "Write", material, false)
	after := stageSnapshot(t, root)
	actual, found, err := runtime.FindSubmission(after.State, packet.ID)
	if err != nil || !found || actual.ControlStatus != "advanced" || actual.Key != prior.Key || actual.EvidenceID != prior.EvidenceID {
		t.Fatalf("original pending not continued: %+v %v stdout=%s stderr=%s", actual, err, out, diagnostic)
	}
	if len(after.State["evidence"].([]any)) != len(before.State["evidence"].([]any)) {
		t.Fatal("natural continuation duplicated reception")
	}
	if _, err := os.Stat(filepath.Join(root, packet.AnswerPath)); !os.IsNotExist(err) {
		t.Fatalf("private answer was recreated: %v", err)
	}
	if life := after.State["lifecycle"].(map[string]any); life["phase"] != "contracts" {
		t.Fatalf("S3 not reached: %+v", life)
	}
}

func TestS2RecordedPendingUsesMainLifecycleRecoveryNotOrdinaryPre(t *testing.T) {
	for _, event := range []string{"SessionStart", "PreCompact", "Stop"} {
		t.Run(event, func(t *testing.T) {
			root, packet := newDesignFixture(t, "none")
			writeContractSource(t, root, "docs/design/README.md", "# Design sources\n")
			fixtures.CommitCurrentFixture(t, root)
			stageJSON(t, root, packet.AnswerPath, designDraft(packet))
			service, err := submission.New(root)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			ctx = &cancelAfterDurableReception{Context: ctx, statePath: service.StatePath, packetID: packet.ID, cancel: cancel}
			if receipt, err := service.SaveDesign(ctx, packet.AnswerPath); err != nil || !receipt.Accepted || receipt.WorkComplete {
				t.Fatalf("original pending: %+v %v", receipt, err)
			}
			if err := os.Remove(filepath.Join(root, packet.AnswerPath)); err != nil {
				t.Fatal(err)
			}
			before := stageSnapshot(t, root)
			planningBoundaryEvent(t, root, "PreToolUse", "Read", "docs/architecture/ARCHITECTURE-039.md", false)
			if after := stageSnapshot(t, root); after.Revision != before.Revision {
				t.Fatalf("unrelated Pre resumed pending: %d -> %d", before.Revision, after.Revision)
			}
			out, diagnostic := planningBoundaryEvent(t, root, event, "", "", false)
			after := stageSnapshot(t, root)
			record, found, err := runtime.FindSubmission(after.State, packet.ID)
			if err != nil || !found || record.ControlStatus != "advanced" {
				t.Fatalf("%s did not continue original pending: %+v %v stdout=%s stderr=%s", event, record, err, out, diagnostic)
			}
			if _, err := os.Stat(filepath.Join(root, packet.AnswerPath)); !os.IsNotExist(err) {
				t.Fatalf("%s recreated private answer: %v", event, err)
			}
		})
	}
}
