package cli_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/entroforge/go-system-builder/internal/checkexec"
	"github.com/entroforge/go-system-builder/internal/integration"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

func cliCheckProfile() checkexec.Profile {
	return checkexec.Profile{ID: "CP-OWNED-GO", Version: "v1", CheckIndex: 0, Command: "go test ./...", Location: "main", TimeoutSeconds: 300, EnvironmentPolicy: "inherited", Environment: []checkexec.EnvFact{}, Dependencies: []checkexec.FileRef{}, Fixtures: []checkexec.FileRef{}, InputClosure: "whole_tree", SideEffects: "unknown", ReusePolicy: "never"}
}
func TestCheckProfileActualMainIntegrationCapturesOriginalResponsibility(t *testing.T) {
	p := cliCheckProfile()
	f := coreBuilderFixtureProfiles(t, []string{p.Command}, []checkexec.Profile{p})
	runCoreBuilderIntegration(t, f)
	b, err := workspace.Decode(stageSnapshot(t, f.root).State)
	if err != nil {
		t.Fatal(err)
	}
	e := b.Executions["assignment-core-builder"]
	if len(e.CheckProfiles) != 1 || e.CheckProfiles[0].Digest() != p.Digest() {
		t.Fatal("original execution did not freeze profile")
	}
	c, err := b.ReadDispatchContext(e)
	if err != nil || len(c.CheckProfiles) != 1 {
		t.Fatal("Worker context lost profile", err)
	}
	cp, found, err := integration.DefaultCheckpointStore().Load(integration.CheckpointPath(f.root, e.RuntimeID, e.BaselineGeneration, e.AssignmentID))
	if err != nil || !found {
		t.Fatal("real integration checkpoint missing", err)
	}
	if len(cp.ToolCheckReceipts) == 0 {
		t.Fatal("actual integration omitted captured tool provenance")
	}
	if err := integration.VerifyCapturedChecks(integration.CheckpointPath(f.root, e.RuntimeID, e.BaselineGeneration, e.AssignmentID), cp); err != nil {
		t.Fatal(err)
	}
	captured := false
	for _, path := range cp.CheckReceipts {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var r struct {
			Profile *checkexec.Observation `json:"profile_observation"`
			Binding checkexec.Binding      `json:"check_binding"`
			Status  string                 `json:"status"`
		}
		if err := json.Unmarshal(data, &r); err != nil {
			t.Fatal(err)
		}
		if r.Profile != nil && r.Profile.ProfileDigest == p.Digest() && r.Status == "pass" {
			if r.Profile.ReuseEligible || r.Binding.AssignmentID != e.AssignmentID || r.Binding.Owner != e.AgentID || r.Binding.Responsibility != "main_required_integration" {
				t.Fatal("profile receipt crossed original responsibility", r)
			}
			captured = true
		}
	}
	if !captured {
		t.Fatal("actual mandatory Main check had no profile observation")
	}
	ref := cp.ToolCheckReceipts[0]
	data, err := os.ReadFile(ref.Path)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	raw["status"] = "fail"
	delete(raw, "receipt_integrity")
	forged, err := checkexec.SealReceipt(raw, raw["output"].(string))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ref.Path, forged, 0600); err != nil {
		t.Fatal(err)
	}
	if err := integration.VerifyCapturedChecks(integration.CheckpointPath(f.root, e.RuntimeID, e.BaselineGeneration, e.AssignmentID), cp); err == nil {
		t.Fatal("resealed forged receipt escaped actual checkpoint binding")
	}
}
func TestCheckProfilePrepareAndReplacementKeepExactProfile(t *testing.T) {
	p := cliCheckProfile()
	f := coreBuilderFixtureProfiles(t, []string{p.Command}, []checkexec.Profile{p})
	old := prepareCoreBuilder(t, f, "")
	b, err := workspace.Decode(stageSnapshot(t, f.root).State)
	if err != nil {
		t.Fatal(err)
	}
	next, err := b.ReplaceExecution(context.Background(), stageSnapshot(t, f.root).State, old.AssignmentID, old.AgentID, old.Generation)
	if err != nil {
		t.Fatal(err)
	}
	if len(next.CheckProfiles) != 1 || next.CheckProfiles[0].Digest() != p.Digest() || next.Generation != old.Generation+1 {
		t.Fatal("replacement refreshed original profile")
	}
	// Unchanged current Main facts are explicitly carried, not inferred later.
	contextPath := filepath.Join(f.root, workspace.DispatchContextSource(next))
	if _, err := os.Stat(contextPath); err != nil {
		t.Fatal("replacement context was not preserved", err)
	}
}

func TestNativeProfileContextExposesFrozenOriginalProfile(t *testing.T) {
	p := cliCheckProfile()
	f := coreBuilderFixtureProfiles(t, []string{p.Command}, []checkexec.Profile{p})
	output := stageCLI(t, "runtime", "assignment-context", "prepare", "--root", f.root, "--agent-id", "core-builder")
	var binding struct {
		Context struct {
			Path string `json:"path"`
		} `json:"context_ref"`
	}
	if err := json.Unmarshal([]byte(output), &binding); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(f.root, binding.Context.Path))
	if err != nil {
		t.Fatal(err)
	}
	var c struct {
		Profiles []checkexec.Profile `json:"check_profiles"`
	}
	if err := json.Unmarshal(data, &c); err != nil {
		t.Fatal(err)
	}
	if len(c.Profiles) != 1 || c.Profiles[0].Digest() != p.Digest() {
		t.Fatal("native omitted or changed original profile")
	}
	before := stageSnapshot(t, f.root)
	if replay := stageCLI(t, "runtime", "assignment-context", "prepare", "--root", f.root, "--agent-id", "core-builder"); replay != output || stageSnapshot(t, f.root).Revision != before.Revision {
		t.Fatal("native profile replay wrote new facts")
	}
}
