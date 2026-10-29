package cli_test

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/entroforge/go-system-builder/internal/cli"
	"github.com/entroforge/go-system-builder/internal/semantic"
)

func TestIndependentS5ReviewCheckReturnsCurrentTaskContextAnalysis(t *testing.T) {
	f := newStageAnswerFixture(t)
	p := f.packets[0]
	e := f.prepare(t, p)
	before := stageSnapshot(t, f.root)
	var out, errOut bytes.Buffer
	code := cli.Run([]string{"runtime", "workspace", "review-check", "--root", e.Path, "--assignment", e.AssignmentID, "--agent", e.AgentID}, nil, &out, &errOut)
	var result semantic.TaskCheckResult
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("review-check missing analysis: code=%d %v %s", code, err, errOut.String())
	}
	if result.ContextAnalysisVersion != semantic.TaskContextAnalysisVersion || len(result.TaskContexts) == 0 {
		t.Fatalf("issued reviewer did not receive versioned task analysis: %+v", result)
	}
	if (code == 0) != (len(result.Problems) == 0) {
		t.Fatal("advisory replaced original tasks check verdict")
	}
	if stageSnapshot(t, f.root).Revision != before.Revision {
		t.Fatal("review analysis wrote Runtime")
	}
}
