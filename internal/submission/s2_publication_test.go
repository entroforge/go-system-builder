package submission

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"

	loopruntime "github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

func designPublicationFixture(t *testing.T) (*Service, workpacket.DesignPacket) {
	t.Helper()
	s := planningIssueFixture(t)
	raw, err := os.ReadFile(s.StatePath)
	if err != nil {
		t.Fatal(err)
	}
	var state map[string]any
	if err := json.Unmarshal(raw, &state); err != nil {
		t.Fatal(err)
	}
	state["lifecycle"] = map[string]any{"state": "planning", "phase": "design", "phase_revision": 3}
	state["bound_req"].(map[string]any)["metadata"].(map[string]any)["ui_impact"] = "unknown"
	raw, err = json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.StatePath, raw, 0600); err != nil {
		t.Fatal(err)
	}
	p, err := loopruntime.CurrentDesignPacket(state)
	if err != nil {
		t.Fatal(err)
	}
	return s, p
}

func TestDesignWriterPreservesOccupiedOriginalFilesAndRegistersActualPaths(t *testing.T) {
	for _, place := range []string{"view", "answer", "view_root", "answer_root"} {
		t.Run(place, func(t *testing.T) {
			s, p := designPublicationFixture(t)
			path := p.ViewPath
			switch place {
			case "answer":
				path = p.AnswerPath
			case "view_root":
				path = ".claude/work-packets"
			case "answer_root":
				path = ".claude/submissions"
			}
			full := filepath.Join(s.Root, path)
			if err := os.MkdirAll(filepath.Dir(full), 0700); err != nil {
				t.Fatal(err)
			}
			unknown := []byte("unrelated owner, keep unchanged")
			if err := os.WriteFile(full, unknown, 0600); err != nil {
				t.Fatal(err)
			}
			issued, err := s.IssueDesign(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if issued.ViewPath == p.ViewPath || issued.AnswerPath == p.AnswerPath {
				t.Fatalf("unknown occupancy was claimed: %+v", issued)
			}
			if got, err := os.ReadFile(full); err != nil || !bytes.Equal(got, unknown) {
				t.Fatalf("unknown occupant altered: %q %v", got, err)
			}
			snap, err := s.snapshot()
			if err != nil {
				t.Fatal(err)
			}
			stored, found, err := loopruntime.FindDesignPacket(snap.State, issued.ID)
			if err != nil || !found || stored.ViewPath != issued.ViewPath || stored.AnswerPath != issued.AnswerPath {
				t.Fatalf("actual paths not registered: %+v %v %v", stored, found, err)
			}
			for _, path := range []string{issued.ViewPath, issued.AnswerPath} {
				if _, err := os.Stat(filepath.Join(s.Root, path)); err != nil {
					t.Fatalf("published original missing %s: %v", path, err)
				}
			}
		})
	}
}

func TestDesignIssuerConcurrentUsesOneRegisteredOriginal(t *testing.T) {
	s, _ := designPublicationFixture(t)
	type result struct {
		packet workpacket.DesignPacket
		err    error
	}
	start := make(chan struct{})
	results := make(chan result, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); <-start; p, e := s.IssueDesign(context.Background()); results <- result{p, e} }()
	}
	close(start)
	wg.Wait()
	close(results)
	var first workpacket.DesignPacket
	for result := range results {
		if result.err != nil {
			t.Fatal(result.err)
		}
		if first.ID == "" {
			first = result.packet
		} else if first.ID != result.packet.ID || first.ViewPath != result.packet.ViewPath || first.AnswerPath != result.packet.AnswerPath {
			t.Fatalf("different registered winners: %+v %+v", first, result.packet)
		}
	}
	snap, err := s.snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if rows := snap.State["entities"].(map[string]any)["work_packets"].([]any); len(rows) != 1 {
		t.Fatalf("issued multiple responsibilities: %#v", rows)
	}
}

func TestDesignReceptionPreservesUnknownEvidenceAndContinuesWithoutPrivateAnswer(t *testing.T) {
	s, _ := designPublicationFixture(t)
	p, err := s.IssueDesign(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	conclusion, architecture := "pass", "docs/architecture/ARCHITECTURE-001.md"
	draft := p.Draft
	draft.Answer = workpacket.DesignAnswer{Conclusion: &conclusion, ArchitecturePath: &architecture}
	data, err := json.Marshal(draft)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.Root, p.AnswerPath), data, 0600); err != nil {
		t.Fatal(err)
	}
	occupied := filepath.Join(s.Root, ".claude/evidence/submissions", p.ID, "answer.json")
	if err := os.MkdirAll(filepath.Dir(occupied), 0700); err != nil {
		t.Fatal(err)
	}
	unknown := []byte("unknown old file")
	if err := os.WriteFile(occupied, unknown, 0600); err != nil {
		t.Fatal(err)
	}
	receipt, err := s.SaveDesign(context.Background(), p.AnswerPath)
	if err != nil || !receipt.Accepted {
		t.Fatalf("original reception: %+v %v", receipt, err)
	}
	if got, err := os.ReadFile(occupied); err != nil || !bytes.Equal(got, unknown) {
		t.Fatalf("unknown report changed: %q %v", got, err)
	}
	snap, err := s.snapshot()
	if err != nil {
		t.Fatal(err)
	}
	record, found, err := loopruntime.FindSubmission(snap.State, p.ID)
	if err != nil || !found || record.ControlStatus != "pending" {
		t.Fatalf("missing pending original reception: %+v %v %v", record, found, err)
	}
	for _, raw := range snap.State["evidence"].([]any) {
		row := raw.(map[string]any)
		if row["id"] == record.EvidenceID {
			if row["path"] == filepath.ToSlash(filepath.Join(".claude/evidence/submissions", p.ID, "answer.json")) {
				t.Fatal("claimed unknown evidence path")
			}
			if _, err := os.Stat(filepath.Join(s.Root, row["path"].(string))); err != nil {
				t.Fatalf("registered evidence missing: %v", err)
			}
		}
	}
	if err := os.Remove(filepath.Join(s.Root, p.AnswerPath)); err != nil {
		t.Fatal(err)
	}
	again, err := s.ContinueDesign(context.Background(), p.ID)
	if err != nil || !again.Accepted || again.SubmissionKey != receipt.SubmissionKey {
		t.Fatalf("accepted followup depended on private answer: %+v %v", again, err)
	}
	if _, err := os.Stat(filepath.Join(s.Root, p.AnswerPath)); !os.IsNotExist(err) {
		t.Fatalf("recreated author answer: %v", err)
	}
}

func TestDesignWriterDoesNotClaimMatchingOrNonFileOccupants(t *testing.T) {
	for _, kind := range []string{"same_view", "same_answer", "directory", "symlink"} {
		t.Run(kind, func(t *testing.T) {
			s, p := designPublicationFixture(t)
			path := p.ViewPath
			if kind == "same_answer" {
				path = p.AnswerPath
			}
			full := filepath.Join(s.Root, path)
			if err := os.MkdirAll(filepath.Dir(full), 0700); err != nil {
				t.Fatal(err)
			}
			var original []byte
			switch kind {
			case "same_view":
				original, _ = json.MarshalIndent(p, "", "  ")
				original = append(original, '\n')
				if err := os.WriteFile(full, original, 0600); err != nil {
					t.Fatal(err)
				}
			case "same_answer":
				original, _ = json.MarshalIndent(p.Draft, "", "  ")
				original = append(original, '\n')
				if err := os.WriteFile(full, original, 0600); err != nil {
					t.Fatal(err)
				}
			case "directory":
				if err := os.Mkdir(full, 0700); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				if err := os.Symlink("elsewhere", full); err != nil {
					t.Fatal(err)
				}
			}
			issued, err := s.IssueDesign(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if issued.ViewPath == p.ViewPath || issued.AnswerPath == p.AnswerPath {
				t.Fatalf("claimed unknown %s", kind)
			}
			info, err := os.Lstat(full)
			if err != nil {
				t.Fatal(err)
			}
			if kind == "directory" && !info.IsDir() {
				t.Fatal("unknown directory altered")
			}
			if kind == "symlink" && info.Mode()&os.ModeSymlink == 0 {
				t.Fatal("unknown link altered")
			}
			if original != nil {
				got, err := os.ReadFile(full)
				if err != nil || !bytes.Equal(got, original) {
					t.Fatalf("unknown file altered: %v", err)
				}
			}
		})
	}
}

func TestDesignIssuerRetainsOriginalTemplateAndNeverRecreatesMissingAnswer(t *testing.T) {
	s, _ := designPublicationFixture(t)
	p, err := s.IssueDesign(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(s.StatePath)
	if err != nil {
		t.Fatal(err)
	}
	var state map[string]any
	if err := json.Unmarshal(raw, &state); err != nil {
		t.Fatal(err)
	}
	rows := state["entities"].(map[string]any)["work_packets"].([]any)
	rows[0].(map[string]any)["draft"].(map[string]any)["instruction"] = "original issued instruction"
	raw, err = json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.StatePath, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(s.Root, p.AnswerPath)); err != nil {
		t.Fatal(err)
	}
	got, err := s.IssueDesign(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got.Draft.Instruction != "original issued instruction" || got.ViewPath != p.ViewPath || got.AnswerPath != p.AnswerPath {
		t.Fatalf("did not preserve original issuance: %+v", got)
	}
	if _, err := os.Stat(filepath.Join(s.Root, p.AnswerPath)); !os.IsNotExist(err) {
		t.Fatalf("missing professional answer recreated: %v", err)
	}
}

func TestCurrentDesignSelectorKeepsOriginalResponsibilityWithoutRebuilding(t *testing.T) {
	s, _ := designPublicationFixture(t)
	p, err := s.IssueDesign(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	snap, err := s.snapshot()
	if err != nil {
		t.Fatal(err)
	}
	rows := snap.State["entities"].(map[string]any)["work_packets"].([]any)
	draft := rows[0].(map[string]any)["draft"].(map[string]any)
	draft["instruction"] = "previous original instruction"
	got, found, err := loopruntime.CurrentIssuedDesignPacket(snap.State)
	if err != nil || !found || got.ID != p.ID || got.Draft.Instruction != "previous original instruction" {
		t.Fatalf("original responsibility lost: %+v found=%v err=%v", got, found, err)
	}
	binding := rows[0].(map[string]any)["binding"].(map[string]any)
	for _, change := range []struct {
		field string
		value any
	}{{"owner", "other-author"}, {"requirement", map[string]any{"id": "other-REQ", "path": "other.md", "version": "v1"}}} {
		original := binding[change.field]
		binding[change.field] = change.value
		if _, found, err := loopruntime.CurrentIssuedDesignPacket(snap.State); err != nil || found {
			t.Fatalf("different responsibility became Main: field=%s found=%v err=%v", change.field, found, err)
		}
		binding[change.field] = original
	}
}
