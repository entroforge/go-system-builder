package acceptance

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCanonicalResultCannotBecomeEmptyS10Baseline(t *testing.T) {
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
			hash := sha256.Sum256(data)
			state := map[string]any{
				"baseline": map[string]any{"generation": 1},
				"evidence": []any{map[string]any{
					"id": "canonical-result", "kind": "completion_report", "path": "result.json",
					"sha256": hex.EncodeToString(hash[:]), "baseline_generation": 1,
					"status": "valid", "invalidated_by": nil,
				}},
			}
			for _, affected := range [][]string{nil, {"all"}} {
				baseline, err := BuildS10ExternalBaseline(root, state, affected)
				if err == nil || !strings.Contains(err.Error(), "original Main facts") || len(baseline.ChangedPaths) != 0 {
					t.Fatalf("canonical Result must fail closed: affected=%v baseline=%v err=%v", affected, baseline, err)
				}
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
			"sha256": strings.Repeat("a", 64), "baseline_generation": 1, "status": "valid",
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
