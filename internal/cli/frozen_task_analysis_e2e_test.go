package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/entroforge/go-system-builder/internal/cli"
	"github.com/entroforge/go-system-builder/internal/semantic"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

func TestS5FrozenTaskAnalysisSurvivesRevisionAndRejectsLostBytes(t *testing.T) {
	f := newStageAnswerFixture(t)
	p := f.packets[0]
	if p.TaskAnalysis == nil {
		t.Fatal("new S5 packet has no frozen analysis")
	}
	artifact, err := workpacket.ReadTaskAnalysis(f.root, p)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(artifact.Inputs, p.Inputs) || artifact.ParserVersion != semantic.TaskContextAnalysisVersion {
		t.Fatal("analysis is not bound to exact formal subjects")
	}
	e := f.prepare(t, p)
	installed, err := os.ReadFile(filepath.Join(e.Path, ".claude/inputs", p.TaskAnalysis.Path))
	if err != nil || workpacket.Digest(installed) != p.TaskAnalysis.SHA256 {
		t.Fatal("analysis not installed as frozen Worker input", err)
	}
	f.begin(t, p, &e)
	r := saveStageHook(t, f.root, e.Path, e.AgentID, workpacket.WorkerAnswerPath, completedDraft(p, "pass"))
	if !r.WorkComplete {
		t.Fatal("original S5 receiver failed", r)
	}
	output := stageCLI(t, "runtime", "stage-answer", "revise", "--root", f.root, "--previous", r.SubmissionKey, "--reason", "Clarify the original TASK execution observation")
	var next workpacket.Packet
	if err := json.Unmarshal([]byte(output), &next); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(next.TaskAnalysis, p.TaskAnalysis) || next.ID == p.ID {
		t.Fatal("named revision refreshed or dropped frozen analysis")
	}
	analysisPath := filepath.Join(f.root, p.TaskAnalysis.Path)
	original, err := os.ReadFile(analysisPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(analysisPath, []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	before := stageSnapshot(t, f.root)
	var out, errOut bytes.Buffer
	if code := cli.Run([]string{"runtime", "stage-answer", "issue", "--root", f.root}, nil, &out, &errOut); code == 0 {
		t.Fatal("corrupt original analysis was silently regenerated")
	}
	if stageSnapshot(t, f.root).Revision != before.Revision {
		t.Fatal("failed source restoration wrote Runtime")
	}
	if err := os.WriteFile(analysisPath, original, 0600); err != nil {
		t.Fatal(err)
	}
	stageCLI(t, "runtime", "stage-answer", "issue", "--root", f.root)
	if stageSnapshot(t, f.root).Revision != before.Revision {
		t.Fatal("immutable issue retry wrote a new analysis packet")
	}
}
