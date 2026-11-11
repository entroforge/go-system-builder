package acceptance

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestS10BaselineKeepsAvailableCompletionPathsWithoutOldProof(t *testing.T) {
	for _, body := range []string{
		`{"kind":"completion_report","assignment_record":{"assignment_revision":2}}`,
		`{"kind":"completion_report","assignment_record":null,"changed_paths":[]}`,
		`{"kind":"completion_report","assignment_record":{},"changed_paths":["src/claimed.go"]}`,
	} {
		t.Run(body, func(t *testing.T) {
			root := t.TempDir()
			data := []byte(body)
			if err := os.WriteFile(filepath.Join(root, "result.json"), data, 0600); err != nil {
				t.Fatal(err)
			}
			state := map[string]any{
				"baseline": map[string]any{"generation": 1},
				"evidence": []any{map[string]any{
					"id": "canonical-result", "kind": "completion_report", "path": "result.json",
					"baseline_generation": 1,
					"status":              "valid", "invalidated_by": nil,
				}},
			}
			baseline, err := BuildS10ExternalBaseline(root, state, nil)
			if err != nil {
				t.Fatalf("actual completion paths became dependent on old source proof: %v", err)
			}
			want := strings.Contains(body, "src/claimed.go")
			if got := len(baseline.ChangedPaths) == 1 && baseline.ChangedPaths[0] == "src/claimed.go"; got != want {
				t.Fatalf("available completion paths = %v", baseline.ChangedPaths)
			}
		})
	}
}

func TestS10AllStillVerifiesCompletionAuthority(t *testing.T) {
	root := t.TempDir()
	state := map[string]any{
		"baseline": map[string]any{"generation": 1},
		"evidence": []any{map[string]any{
			"id": "missing", "kind": "completion_report", "path": "missing.json",
			"baseline_generation": 1, "status": "valid",
		}},
	}
	if _, err := BuildS10ExternalBaseline(root, state, []string{"all"}); err == nil {
		t.Fatal("all must not bypass missing original completion evidence")
	}
	state["evidence"] = []any{}
	baseline, err := BuildS10ExternalBaseline(root, state, []string{"all"})
	if err != nil || !baseline.AffectedPathsAll || len(baseline.ChangedPaths) != 0 {
		t.Fatalf("verified all sweep changed: %+v %v", baseline, err)
	}
}
