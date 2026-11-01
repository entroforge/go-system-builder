package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestS8GrantOriginalWriterPreservesUnknownAndReplays(t *testing.T) {
	for _, occupied := range []string{"file", "parent"} {
		t.Run(occupied, func(t *testing.T) {
			root, contract := s8LeadContractTestReady(t)
			writeRepairLeadJSON(t, contract, s8LeadContractTestAnswer())
			s8LeadContractTestSave(t, root, contract)
			current := investigationLeadTestSnapshot(t, root)
			path := repairDelegationApplicationPaths(root, current.State)[0]
			answer := repairDelegationApplicationAnswer{Decision: "delegate_bounded_repair", Reviewer: "driver-reviewer", AllowedPaths: []string{"internal/example"}, ForbiddenPaths: []string{"internal/unrelated-private"}, MaxContracts: 2, ExpiresAt: time.Now().Add(time.Hour).UTC().Format(time.RFC3339)}
			writeRepairLeadJSON(t, path, answer)
			grant := repairDelegationFromApplication(root, current.State, answer)
			dir := s8OriginalSourceDir(root, grant.RuntimeID, grant.BaselineGeneration, grant.DecisionID)
			obstruction := filepath.Join(dir, "grant.json")
			if occupied == "parent" {
				obstruction = dir
			}
			if err := os.MkdirAll(filepath.Dir(obstruction), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(obstruction, []byte("unknown user material"), 0600); err != nil {
				t.Fatal(err)
			}
			out := s8DecisionNormalHook(t, root, path)
			if !strings.Contains(out, "finite grant was recorded") {
				t.Fatalf("grant not received: %s", out)
			}
			if data, err := os.ReadFile(obstruction); err != nil || string(data) != "unknown user material" {
				t.Fatalf("unknown occupant changed: %q %v", data, err)
			}
			accepted := investigationLeadTestSnapshot(t, root)
			grant = repairDelegationFromApplication(root, accepted.State, answer)
			row := evidenceEntryByID(accepted.State, grant.DecisionID)
			if row == nil || samePath(filepath.Join(root, stringValue(row["path"])), obstruction) {
				t.Fatalf("actual registered grant path missing: %#v", row)
			}
			data, err := os.ReadFile(filepath.Join(root, stringValue(row["path"])))
			if err != nil {
				t.Fatal(err)
			}
			var stored map[string]any
			if json.Unmarshal(data, &stored) != nil || stored["reviewer"] != answer.Reviewer || stored["approved_by"] != grant.ApprovedBy {
				t.Fatalf("human authority lost: %s", data)
			}
			before := s8DecisionControlBytes(t, root)
			s8DecisionNormalHook(t, root, path)
			s8DecisionRequireUnchanged(t, root, before)
		})
	}
}

func TestS8GrantOriginalWriterConcurrentReception(t *testing.T) {
	root, contract := s8LeadContractTestReady(t)
	writeRepairLeadJSON(t, contract, s8LeadContractTestAnswer())
	s8LeadContractTestSave(t, root, contract)
	current := investigationLeadTestSnapshot(t, root)
	path := repairDelegationApplicationPaths(root, current.State)[0]
	answer := repairDelegationApplicationAnswer{Decision: "delegate_bounded_repair", Reviewer: "driver-reviewer", AllowedPaths: []string{"internal/example"}, ForbiddenPaths: []string{"internal/unrelated-private"}, MaxContracts: 2, ExpiresAt: time.Now().Add(time.Hour).UTC().Format(time.RFC3339)}
	writeRepairLeadJSON(t, path, answer)
	payload, _ := json.Marshal(map[string]any{"hook_event_name": "PostToolUse", "cwd": root, "tool_name": "Write", "tool_input": map[string]any{"file_path": path}, "tool_response": map[string]any{"file_path": path}})
	var wg sync.WaitGroup
	outputs := make([]string, 2)
	for i := range outputs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			var out, diag bytes.Buffer
			code := Run([]string{"hook", "--event", "PostToolUse", "--root", root}, bytes.NewReader(payload), &out, &diag)
			outputs[i] = out.String() + diag.String()
			if code != 0 {
				outputs[i] += " hook failed"
			}
		}(i)
	}
	wg.Wait()
	accepted := investigationLeadTestSnapshot(t, root)
	grant := repairDelegationFromApplication(root, accepted.State, answer)
	row := evidenceEntryByID(accepted.State, grant.DecisionID)
	if row == nil {
		t.Fatalf("concurrent original authority not recorded: %v", outputs)
	}
	count := 0
	for _, evidence := range objectSliceAny(accepted.State["evidence"]) {
		if evidence["id"] == grant.DecisionID {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("duplicate authority: %d", count)
	}
	before := s8DecisionControlBytes(t, root)
	s8DecisionNormalHook(t, root, path)
	s8DecisionRequireUnchanged(t, root, before)
}
