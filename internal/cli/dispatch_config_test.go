package cli

import (
	"bytes"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDispatchInitializationExplicitFrozenAndInvalidThresholds(t *testing.T) {
	for _, args := range [][]string{{"--dispatch-max-unreceived-results", "0"}, {"--dispatch-resume-unreceived-results", "1"}, {"--dispatch-max-unreceived-results", "2", "--dispatch-resume-unreceived-results", "2"}} {
		root := initFormatRoot(t)
		var out, stderr bytes.Buffer
		if code := Run(append([]string{"init", "--root", root}, args...), nil, &out, &stderr); code != 2 {
			t.Fatalf("invalid thresholds %v: %d %s", args, code, stderr.String())
		}
		if _, err := os.Stat(filepath.Join(root, ".claude/loop-state.json")); !os.IsNotExist(err) {
			t.Fatal("invalid initialization published state")
		}
	}
	root := initFormatRoot(t)
	var out, stderr bytes.Buffer
	args := []string{"init", "--root", root, "--dispatch-max-unreceived-results", "3", "--dispatch-resume-unreceived-results", "1"}
	if code := Run(args, nil, &out, &stderr); code != 0 {
		t.Fatalf("%d %s", code, stderr.String())
	}
	c, err := runtime.DispatchConfig(initMap(t, root))
	if err != nil || c == nil || c.MaxUnreceivedResults != 3 || c.ResumeUnreceivedResults != 1 {
		t.Fatal(c, err)
	}
	before, _ := os.ReadFile(filepath.Join(root, ".claude/loop-state.json"))
	if code := Run([]string{"init", "--root", root, "--dispatch-max-unreceived-results", "5"}, nil, &out, &stderr); code == 0 {
		t.Fatal("existing Runtime configuration changed")
	}
	after, _ := os.ReadFile(filepath.Join(root, ".claude/loop-state.json"))
	if !bytes.Equal(before, after) {
		t.Fatal("refused init changed state")
	}
}

func TestDispatchPendingInitializationMustKeepOriginalSelection(t *testing.T) {
	root := initFormatRoot(t)
	fresh, err := inactiveRuntimeState(root, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	fresh["configuration"].(map[string]any)["optimization"].(map[string]any)["dispatch_backpressure"] = map[string]any{"max_unreceived_results": 3, "resume_unreceived_results": 1}
	marker := filepath.Join(root, ".claude/loop-init-pending.json")
	putInitMap(t, marker, initPending{SchemaVersion: "1.0.0", FreshState: fresh})
	before, _ := os.ReadFile(marker)
	var out, stderr bytes.Buffer
	if code := Run([]string{"init", "--root", root, "--dispatch-max-unreceived-results", "2"}, nil, &out, &stderr); code == 0 {
		t.Fatal("pending selection changed")
	}
	after, _ := os.ReadFile(marker)
	if !bytes.Equal(before, after) {
		t.Fatal("refused retry replaced marker")
	}
	if code := Run([]string{"init", "--root", root}, nil, &out, &stderr); code != 0 {
		t.Fatalf("original retry failed: %d %s", code, stderr.String())
	}
	c, err := runtime.DispatchConfig(initMap(t, root))
	if err != nil || c == nil || c.MaxUnreceivedResults != 3 || c.ResumeUnreceivedResults != 1 {
		t.Fatal(c, err)
	}
}
