package repair_test

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/entroforge/go-system-builder/internal/cli"
	"github.com/entroforge/go-system-builder/internal/repair"
)

func TestRepairOperationPublicCLI(t *testing.T) {
	root, sp, jp := repairOperationFixture(t)
	run := func(args ...string) map[string]json.RawMessage {
		t.Helper()
		var stdout, stderr bytes.Buffer
		if binary := os.Getenv("FRAMEWORK_REPAIR_BINARY"); binary != "" {
			cmd := exec.Command(binary, args...)
			cmd.Stdout, cmd.Stderr = &stdout, &stderr
			if err := cmd.Run(); err != nil {
				t.Fatalf("%v: %v %s", args, err, stderr.String())
			}
		} else if code := cli.Run(args, bytes.NewReader(nil), &stdout, &stderr); code != 0 {
			t.Fatalf("%v: exit=%d %s", args, code, stderr.String())
		}
		var response map[string]json.RawMessage
		if err := json.Unmarshal(stdout.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		return response
	}
	requestFile := func(value any) string {
		t.Helper()
		dir := filepath.Join(root, ".claude/review/repair/drafts")
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		file, err := os.CreateTemp(dir, "request-*.json")
		if err != nil {
			t.Fatal(err)
		}
		path := file.Name()
		if err := file.Close(); err != nil {
			t.Fatal(err)
		}
		data, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0644); err != nil {
			t.Fatal(err)
		}
		return path
	}
	refFrom := func(response map[string]json.RawMessage) repair.ArtifactRef {
		var ref repair.ArtifactRef
		if err := json.Unmarshal(response["artifact_ref"], &ref); err != nil {
			t.Fatal(err)
		}
		return ref
	}
	requests := [][]string{
		{"runtime", "repair", "session", "open", "--root", root, "--session-id", "repair-session-cli-op", "--created-by", "main", "--operation-id", "cli-open", "--expected-revision", "0"},
		{"runtime", "repair", "plan", "compile", "--root", root, "--plan-id", "repair-plan-cli-op", "--created-by", "main", "--operation-id", "cli-plan", "--expected-revision", "1"},
	}
	responses := []map[string]json.RawMessage{run(requests[0]...), run(requests[1]...)}
	report := repair.PlanReportRequest{Session: refFrom(responses[0]), Plan: refFrom(responses[1]), AssignmentID: "repair-assignment-unit-1", AgentID: "builder-1", ReportID: "repair-plan-report-cli-op", PlanText: "restore authority", RedChecks: []repair.RepairCheck{{Name: "before", Command: "go test ./internal/api", Result: "fail", EvidenceRefs: []string{"test://red"}}}, ProposedPaths: []string{"internal/api/payload.go"}}
	requests = append(requests, []string{"runtime", "repair", "plan-report", "submit", "--root", root, "--file", requestFile(report), "--operation-id", "cli-report", "--expected-revision", "2"})
	responses = append(responses, run(requests[2]...))
	run("runtime", "repair", "execution", "begin", "--root", root, "--actor", "main", "--expected-revision", "3")
	product := []byte("package api\n")
	writeFile(t, root, "internal/api/payload.go", string(product))
	// The default Changeset path must resolve the new scoped Session through
	// the authoritative pointer, while explicit refs support historical authoring.
	run("runtime", "repair", "changeset", "compute", "--root", root, "--session-id", "repair-session-cli-op")
	result := repair.RepairResultRequest{ResultID: "repair-result-cli-op", ProducerAgentID: "builder-1", UnitResults: []repair.RepairUnitResult{{UnitID: "unit-1", Status: "pass", EvidenceRefs: []string{"test://unit"}}}, ChangedArtifacts: []repair.ChangedArtifact{{Path: "internal/api/payload.go", SHA256: fileHash(product), Status: "added"}}, Checks: []repair.RepairCheck{{Name: "after", Command: "go test ./internal/api", Result: "pass", EvidenceRefs: []string{"test://green"}}}, Result: "pass"}
	requests = append(requests, []string{"runtime", "repair", "result", "submit", "--root", root, "--file", requestFile(result), "--operation-id", "cli-result", "--expected-revision", "4"})
	responses = append(responses, run(requests[3]...))
	beforeState, _ := os.ReadFile(sp)
	beforeJournal, _ := os.ReadFile(jp)
	for i, args := range requests {
		replayed := run(args...)
		if len(responses[i]["operation_receipt"]) == 0 || bytes.Equal(responses[i]["operation_receipt"], []byte("null")) || !bytes.Equal(responses[i]["operation_receipt"], replayed["operation_receipt"]) || !bytes.Equal(responses[i]["artifact_ref"], replayed["artifact_ref"]) {
			t.Fatalf("operation %d lost its original receipt or artifact", i)
		}
	}
	query := run("runtime", "operation", "--root", root, "--id", "cli-result")
	if string(query["status"]) != `"committed"` || !bytes.Equal(query["receipt"], responses[3]["operation_receipt"]) {
		t.Fatalf("operation query differs: %s", query["receipt"])
	}
	afterState, _ := os.ReadFile(sp)
	afterJournal, _ := os.ReadFile(jp)
	if !bytes.Equal(beforeState, afterState) || !bytes.Equal(beforeJournal, afterJournal) {
		t.Fatal("public CLI retries consumed state/journal twice")
	}
}
