package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/repair"
)

func TestNativeS9CandidatePreparesImpactWithoutRecordingMainCompletion(t *testing.T) {
	root, worker, packet, input, _ := nativeS9WorkerFixture(t)
	// Source/Git qualification must not enter the original reception chain.
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte("#!/bin/sh\nexit 97\n"), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	input.Event, input.CWD = "SubagentStop", worker
	var output, diagnostic bytes.Buffer
	if handled, code := nativeS9Hook(root, input, &output, &diagnostic); !handled || code != 0 {
		t.Fatalf("Candidate reception: %t %d %s %s", handled, code, &output, &diagnostic)
	}
	snapshot := repairDispatchSnapshot(t, root)
	pointer := repairPointerCLI(snapshot.State)
	if stringValue(pointer["result_ref"]) != "" || stringValue(pointer["impact_ref"]) != "" {
		t.Fatalf("Candidate preparation invented Main completion: %+v", pointer)
	}
	contributions, err := repair.ReceivedRepairContributions(root, snapshot.State)
	if err != nil || len(contributions) != 1 {
		t.Fatalf("accepted contribution: %+v %v; diagnostic: %s", contributions, err, &diagnostic)
	}
	path, err := repairLeadQuestionnairePath(root, snapshot.State, "change-impact.json")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("Main's original Impact was not prepared: %v; %s", err, &diagnostic)
	}
	// Git is needed only when Main actually begins integration. Its failure
	// must leave the already accepted Result and Impact preparation intact.
	var mainOut, mainErr bytes.Buffer
	if code := runWorkspaceDelivery([]string{"integrate", "--root", root, "--assignment", packet.Binding.ManifestAssignmentID, "--agent", packet.Binding.OwnerAgentID, "--check-command", "true"}, &mainOut, &mainErr); code == 0 || !strings.Contains(mainErr.String(), "git") {
		t.Fatalf("unavailable Git was not reported at actual integration: code=%d out=%s err=%s", code, &mainOut, &mainErr)
	}
	after, err := repair.ReceivedRepairContributions(root, repairDispatchSnapshot(t, root).State)
	if err != nil || len(after) != 1 {
		t.Fatalf("Git failure erased accepted Result: %+v %v", after, err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("Git failure erased Impact preparation: %v", err)
	}
}
