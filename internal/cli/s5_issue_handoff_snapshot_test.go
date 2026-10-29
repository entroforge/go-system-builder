package cli

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestS5IssueHandoffLatestStageSuppressesOldDispatch(t *testing.T) {
	for _, preparationErr := range []error{nil, errors.New("original preparation deadline")} {
		t.Run(map[bool]string{true: "failed_attempt", false: "successful_attempt"}[preparationErr != nil], func(t *testing.T) {
			root := t.TempDir()
			// The old S5 row deliberately cannot be decoded as a packet. The latest
			// lifecycle must take precedence before any obsolete packet projection.
			state := map[string]any{"revision": 7, "lifecycle": map[string]any{"state": "building", "phase": "", "phase_revision": 3}, "entities": map[string]any{"work_packets": []any{map[string]any{"template_id": "s5-document-review-v1", "packet_id": "obsolete-s5"}}}}
			data, err := json.Marshal(state)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(filepath.Join(root, ".claude"), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, ".claude/loop-state.json"), data, 0600); err != nil {
				t.Fatal(err)
			}
			packets, guidance, err := s5IssueHandoffContext(context.Background(), root, preparationErr)
			if err != nil {
				t.Fatal(err)
			}
			encoded, err := json.Marshal(packets)
			if err != nil || string(encoded) != "[]" {
				t.Fatalf("left-S5 machine output is not an empty JSON array: %s %v", encoded, err)
			}
			for _, want := range []string{"当前阶段 S6", "building", "当前阶段原责任"} {
				if !strings.Contains(guidance, want) {
					t.Fatalf("latest stage omitted %q: %s", want, guidance)
				}
			}
			for _, stale := range []string{"stage-answer issue", "separate native Agent context", "S5 原准备已执行", "obsolete-s5"} {
				if strings.Contains(guidance, stale) {
					t.Fatalf("left-S5 output retained obsolete dispatch %q: %s", stale, guidance)
				}
			}
			if preparationErr != nil && !strings.Contains(guidance, preparationErr.Error()) {
				t.Fatal("original attempt error lost", guidance)
			}
		})
	}
}
