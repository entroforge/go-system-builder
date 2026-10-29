package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/cli"
	"github.com/entroforge/go-system-builder/internal/metrics"
	"github.com/entroforge/go-system-builder/internal/runtime"
)

func TestPreCompactIsSilentAndSessionStartRecoversOriginalUnreceivedWork(t *testing.T) {
	for _, stage := range []string{"S2", "S3"} {
		t.Run(stage, func(t *testing.T) {
			var root, view, answer, packetID string
			if stage == "S2" {
				r, packet := newDesignFixture(t, "none")
				root, view, answer, packetID = r, packet.ViewPath, packet.AnswerPath, packet.ID
			} else {
				r, packet := newContractAnswerFixture(t)
				root, view, answer, packetID = r, packet.ViewPath, packet.AnswerPath, packet.ID
				stageJSON(t, root, answer, completedContractDraft(packet))
			}
			statePath, journalPath := filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl")
			before := map[string][]byte{}
			if err := metrics.ObserveRecoveryPacket(root); err != nil {
				t.Fatal(err)
			}
			metricBefore, err := metrics.NewStore(root).Read()
			if err != nil {
				t.Fatal(err)
			}
			for _, path := range []string{statePath, journalPath, filepath.Join(root, answer)} {
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				before[path] = data
			}
			for i := 0; i < 2; i++ {
				body, _ := json.Marshal(map[string]any{"hook_event_name": "PreCompact", "trigger": "auto", "session_id": "r09-main", "cwd": root})
				var out, diagnostic bytes.Buffer
				if code := cli.Run([]string{"hook", "--root", root, "--event", "PreCompact"}, bytes.NewReader(body), &out, &diagnostic); code != 0 || out.Len() != 0 {
					t.Fatalf("PreCompact emitted a recovery promise or ran a control cycle: code=%d stdout=%s stderr=%s", code, &out, &diagnostic)
				}
				for path, want := range before {
					data, err := os.ReadFile(path)
					if err != nil || !bytes.Equal(data, want) {
						t.Fatalf("PreCompact mutated original Runtime/journal/private work: %s %v", path, err)
					}
				}
			}
			if _, err := os.Stat(filepath.Join(root, ".claude/hook-metrics/summary.json")); err != nil {
				t.Fatalf("PreCompact skipped its existing diagnostic maintenance: %v", err)
			}
			metricAfter, err := metrics.NewStore(root).Read()
			if err != nil || metricAfter.RecoveryPackets != metricBefore.RecoveryPackets {
				t.Fatalf("silent compaction invented recovery packets or double-counted observations: %+v %v", metricAfter, err)
			}
			body, _ := json.Marshal(map[string]any{"hook_event_name": "SessionStart", "source": "compact", "session_id": "r09-main", "cwd": root})
			var out, diagnostic bytes.Buffer
			if code := cli.Run([]string{"hook", "--root", root, "--event", "SessionStart"}, bytes.NewReader(body), &out, &diagnostic); code != 0 {
				t.Fatalf("SessionStart could not recover original work: %d %s", code, &diagnostic)
			}
			var wire struct {
				Output struct {
					Context string `json:"additionalContext"`
					Event   string `json:"hookEventName"`
				} `json:"hookSpecificOutput"`
			}
			if err := json.Unmarshal(out.Bytes(), &wire); err != nil || wire.Output.Event != "SessionStart" || !strings.Contains(wire.Output.Context, filepath.Join(root, view)) || !strings.Contains(wire.Output.Context, filepath.Join(root, answer)) {
				t.Fatalf("current original responsibility paths were lost after compaction: %v %s %s", err, &out, &diagnostic)
			}
			if _, accepted, err := runtime.FindSubmission(stageSnapshot(t, root).State, packetID); err != nil || accepted {
				t.Fatalf("compression recovery received an unsubmitted private answer: %t %v", accepted, err)
			}
			if data, err := os.ReadFile(filepath.Join(root, answer)); err != nil || !bytes.Equal(data, before[filepath.Join(root, answer)]) {
				t.Fatalf("SessionStart rewrote private professional work: %v", err)
			}
		})
	}
}
