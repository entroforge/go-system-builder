package cli_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/checkexec"
	"github.com/entroforge/go-system-builder/internal/cli"
)

func TestMainCheckMissingToolRejectsPrepareBeforeReady(t *testing.T) {
	f := coreBuilderFixtureChecks(t, []string{"gsb-definitely-missing-tool-20261013 --run"})
	before := stageSnapshot(t, f.root)
	var out, errOut bytes.Buffer
	code := cli.Run([]string{"runtime", "workspace", "prepare", "--root", f.root, "--assignment", "assignment-core-builder", "--agent", "core-builder", "--check-location", "main"}, nil, &out, &errOut)
	if code == 0 || !strings.Contains(errOut.String(), "CHECK_CAPABILITY_MISSING") {
		t.Fatalf("missing Main check entrance was published ready: %d %s", code, errOut.String())
	}
	if stageSnapshot(t, f.root).Revision != before.Revision {
		t.Fatal("failed Main capability preparation wrote Runtime")
	}
}
func TestMainCheckCapabilitiesObservationIsReadonlyAndPartial(t *testing.T) {
	f := coreBuilderFixture(t)
	e := prepareCoreBuilder(t, f, "")
	before := stageSnapshot(t, f.root)
	output := stageCLI(t, "runtime", "workspace", "observe", "--root", e.Path, "--assignment", e.AssignmentID, "--agent", e.AgentID, "--view", "check-capabilities")
	var report checkexec.CapabilityReport
	if err := json.Unmarshal([]byte(output), &report); err != nil {
		t.Fatal(err)
	}
	if report.Runner != "main_shell" || !report.Available || report.FullyVerified || len(report.Checks) != 1 || report.Checks[0].Tool != "go" {
		t.Fatal("preflight overstated environment proof", report)
	}
	if stageSnapshot(t, f.root).Revision != before.Revision {
		t.Fatal("capability observation wrote Runtime")
	}
}

func TestCheckCapabilityObservationHelpReturnsSuccess(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := cli.Run([]string{"runtime", "workspace", "observe", "--help"}, nil, &out, &errOut); code != 0 || !strings.Contains(out.String()+errOut.String(), "check-capabilities") {
		t.Fatalf("unreadable check entrance help: %d %s %s", code, out.String(), errOut.String())
	}
}

func TestMainCheckReuseObservationIsReadonlyAndCannotSkipRequiredChecks(t *testing.T) {
	p := cliCheckProfile()
	f := coreBuilderFixtureProfiles(t, []string{p.Command}, []checkexec.Profile{p})
	e := prepareCoreBuilder(t, f, "")
	before := stageSnapshot(t, f.root)
	output := stageCLI(t, "runtime", "workspace", "observe", "--root", e.Path, "--assignment", e.AssignmentID, "--agent", e.AgentID, "--view", "check-reuse")
	var report []struct {
		Decision checkexec.ReuseDecision `json:"decision"`
	}
	if err := json.Unmarshal([]byte(output), &report); err != nil {
		t.Fatal(err)
	}
	if len(report) != 1 || report[0].Decision.Action != "run" || report[0].Decision.WouldReuse || report[0].Decision.Reason != "independent_or_mandatory_domain_check" {
		t.Fatal(output)
	}
	if stageSnapshot(t, f.root).Revision != before.Revision {
		t.Fatal("reuse observation wrote runtime")
	}
}
