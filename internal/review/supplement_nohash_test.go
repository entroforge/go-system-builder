package review

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestSupplementOriginalObservationWithoutDigest(t *testing.T) {
	for _, old := range []string{"", `,"hash":null`, `,"hash":"old-wrong"`, `,"hash":7`, `,"hash":{"old":true}`} {
		t.Run(old, func(t *testing.T) {
			root := t.TempDir()
			statePath, journalPath, _ := supplementFixture(t, root)
			file := filepath.Join(root, "observation.json")
			original := []byte(`{"author":"agent-qa-1","new_observation":"The original observation","evidence_refs":["command output: checked"]` + old + `}`)
			if err := os.WriteFile(file, original, 0600); err != nil {
				t.Fatal(err)
			}
			// A stranger's file at the predictable first candidate is never
			// authenticated, overwritten, or allowed to block the author.
			occupied := filepath.Join(root, ".claude/evidence/loop-REQ-TEST/g1/finding-supplements/supplement-g1-r2.json")
			if err := os.MkdirAll(filepath.Dir(occupied), 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(occupied, []byte("unrelated"), 0600); err != nil {
				t.Fatal(err)
			}
			req := SupplementRequest{FindingID: "finding-qa-1", FilePath: file, InRoundNote: true, OccurredAt: time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)}
			first, err := SubmitSupplement(root, statePath, journalPath, req)
			if err != nil {
				t.Fatal(err)
			}
			if filepath.Join(root, filepath.FromSlash(first.Path)) == occupied {
				t.Fatalf("unexpected digest or occupied destination: %+v", first)
			}
			if got, _ := os.ReadFile(occupied); string(got) != "unrelated" {
				t.Fatal("unknown existing file changed")
			}
			if got, _ := os.ReadFile(file); !bytes.Equal(got, original) {
				t.Fatal("author's observation changed")
			}
			stored, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(first.Path)))
			if err != nil {
				t.Fatal(err)
			}
			var document map[string]any
			if err := json.Unmarshal(stored, &document); err != nil {
				t.Fatal(err)
			}
			var authored map[string]any
			if err := json.Unmarshal(original, &authored); err != nil {
				t.Fatal(err)
			}
			originalHash, authoredHash := authored["hash"]
			storedHash, preservedHash := document["hash"]
			if authoredHash != preservedHash || !reflect.DeepEqual(originalHash, storedHash) {
				t.Fatal("supplement changed the author's unrelated hash field")
			}
			before, _ := os.ReadFile(statePath)
			var current map[string]any
			if err := json.Unmarshal(before, &current); err != nil {
				t.Fatal(err)
			}
			entries := current["entities"].(map[string]any)["finding_supplements"].([]any)
			if _, exists := entries[0].(map[string]any)["sha256"]; exists {
				t.Fatal("new supplement index registered sha256")
			}
			for _, entry := range current["evidence"].([]any) {
				item := entry.(map[string]any)
				if item["id"] == first.SupplementID {
					if _, exists := item["sha256"]; exists {
						t.Fatal("new supplement evidence registered sha256")
					}
				}
			}
			retry, err := SubmitSupplement(root, statePath, journalPath, req)
			if err != nil || retry.SupplementID != first.SupplementID || retry.Path != first.Path {
				t.Fatalf("same observation did not reuse original: %+v, %v", retry, err)
			}
			after, _ := os.ReadFile(statePath)
			if !bytes.Equal(before, after) {
				t.Fatal("replay changed Runtime")
			}
			changed := bytes.Replace(original, []byte("The original observation"), []byte("A genuinely new observation"), 1)
			if err := os.WriteFile(file, changed, 0600); err != nil {
				t.Fatal(err)
			}
			fresh, err := SubmitSupplement(root, statePath, journalPath, req)
			if err != nil || fresh.SupplementID == first.SupplementID {
				t.Fatalf("new observation not preserved independently: %+v, %v", fresh, err)
			}
			if got, _ := os.ReadFile(filepath.Join(root, filepath.FromSlash(first.Path))); !bytes.Equal(got, stored) {
				t.Fatal("new observation overwrote original")
			}
		})
	}
}

func TestSupplementConcurrentOriginalObservations(t *testing.T) {
	for _, same := range []bool{true, false} {
		name := "different"
		if same {
			name = "same"
		}
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			statePath, journalPath, _ := supplementFixture(t, root)
			paths := []string{filepath.Join(root, "first.json"), filepath.Join(root, "second.json")}
			for i, path := range paths {
				observation := "first observation"
				if i == 1 && !same {
					observation = "second observation"
				}
				data, err := json.Marshal(map[string]any{"author": "agent-qa-1", "new_observation": observation})
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, data, 0600); err != nil {
					t.Fatal(err)
				}
			}
			type result struct {
				receipt SupplementReceipt
				err     error
			}
			start := make(chan struct{})
			results := make(chan result, len(paths))
			for _, path := range paths {
				go func(path string) {
					<-start
					r, err := SubmitSupplement(root, statePath, journalPath, SupplementRequest{FindingID: "finding-qa-1", FilePath: path, InRoundNote: true})
					results <- result{r, err}
				}(path)
			}
			close(start)
			a, b := <-results, <-results
			if a.err != nil || b.err != nil {
				t.Fatalf("concurrent original submissions failed: %v; %v", a.err, b.err)
			}
			if (a.receipt.SupplementID == b.receipt.SupplementID) != same {
				t.Fatalf("same=%t, receipts=%+v / %+v", same, a.receipt, b.receipt)
			}
			rows, err := SupplementsForFinding(root, statePath, "finding-qa-1")
			want := 2
			if same {
				want = 1
			}
			if err != nil || len(rows) != want {
				t.Fatalf("same=%t, rows=%+v, err=%v", same, rows, err)
			}
		})
	}
}
