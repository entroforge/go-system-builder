package mainreceipt

import (
	"context"
	"encoding/json"
	"os"
	"reflect"
	"testing"

	"github.com/entroforge/go-system-builder/internal/assignmentrecord"
)

func TestOriginalMainFactsIgnoreArbitraryHistoricalMachineValues(t *testing.T) {
	for name, old := range map[string]any{"null": nil, "text": "legacy", "number": 42, "object": map[string]any{"old": true}} {
		t.Run(name, func(t *testing.T) {
			f := newReaderFixture(t)
			path := assignmentrecord.IntegrationCheckpointPath(f.root, f.input.Record.Reference())
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var cp map[string]any
			if err := json.Unmarshal(data, &cp); err != nil {
				t.Fatal(err)
			}
			cp["assignment_record_sha256"], cp["source_head"], cp["completion_report_sha256"] = old, old, old
			facts := cp["canonical_main_facts"].(map[string]any)
			facts["source_head"], facts["changed_paths_sha256"] = old, old
			binding := facts["binding"].(map[string]any)
			binding["result_sha256"], binding["answer_digest"] = old, old
			check := facts["checks"].([]any)[0].(map[string]any)
			check["receipt_sha256"], check["profile_digest"] = old, old
			data, _ = json.Marshal(cp)
			if err := os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
			verified, err := Verify(context.Background(), f.input)
			if err != nil {
				t.Fatalf("original facts rejected: %v", err)
			}
			if len(verified.MainFacts().Checks) == 0 || verified.MainFacts().Checks[0].Status != "pass" {
				t.Fatal("lost actual check")
			}
			after, err := os.ReadFile(path)
			if err != nil || !reflect.DeepEqual(after, data) {
				t.Fatal("rewrote original checkpoint")
			}
			f.input.ExpectedTargetBranch = "another-branch"
			if _, err := Verify(context.Background(), f.input); err == nil {
				t.Fatal("different actual branch accepted")
			}
		})
	}
}
