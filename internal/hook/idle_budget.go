package hook

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"github.com/entroforge/go-system-builder/internal/filelock"
	"github.com/entroforge/go-system-builder/internal/policy"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"os"
	"path/filepath"
	"time"
)

// IdleRecoveryExhausted limits only the framework's own continuation reminders.
// Safety denials are never suppressed. The cache is diagnostic, not Runtime
// evidence: exhausting it permits idle, never completion or product writes.
func IdleRecoveryExhausted(root string, input policy.Input, decision policy.Decision) (bool, error) {
	if input.Event != "TeammateIdle" || decision.RuleID != RuleTeammateIdleResumeAssignment {
		return false, nil
	}
	snapshot, err := runtime.NewStore(filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl")).Snapshot()
	if err != nil {
		return false, err
	}
	entities, _ := snapshot.State["entities"].(map[string]any)
	rows, _ := entities["agents"].([]any)
	var agent map[string]any
	for _, raw := range rows {
		a, _ := raw.(map[string]any)
		if a["id"] == input.EffectiveAgentID() {
			agent = a
			break
		}
	}
	facts := map[string]any{"runtime": snapshot.State["runtime_id"], "baseline": snapshot.State["baseline"], "reason": decision.Reason, "agent": agent}
	// Observer churn alone is not progress.
	if agent != nil {
		copy := map[string]any{}
		for k, v := range agent {
			if k != "updated_at" {
				copy[k] = v
			}
		}
		facts["agent"] = copy
	}
	if agent != nil {
		hashes := map[string]string{}
		for _, key := range []string{"readback_ref", "plan_reported_ref", "activation_ref", "completion_reported_ref"} {
			ref, _ := agent[key].(string)
			if ref != "" {
				p := ref
				if !filepath.IsAbs(p) {
					p = filepath.Join(root, p)
				}
				if b, e := os.ReadFile(p); e == nil {
					hashes[key] = fmt.Sprintf("%x", sha256.Sum256(b))
				}
			}
		}
		facts["evidence_hashes"] = hashes
	}
	data, _ := json.Marshal(facts)
	fingerprint := fmt.Sprintf("%x", sha256.Sum256(data))
	key := fmt.Sprintf("%x", sha256.Sum256([]byte(input.SessionID+"\x00"+input.EffectiveAgentID())))
	dir := filepath.Join(root, ".claude/hook-notices")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return false, err
	}
	path := filepath.Join(dir, "idle-"+key)
	// Concurrent callbacks must not overwrite each other's retry budget.
	budget, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	release, err := filelock.Acquire(budget, path+".lck")
	if err != nil {
		return false, fmt.Errorf("idle recovery cache unavailable: %w", err)
	}
	defer release()
	var record struct {
		Fingerprint string `json:"fingerprint"`
		Count       int    `json:"count"`
	}
	if data, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(data, &record); err != nil {
			return false, err
		}
	} else if !os.IsNotExist(err) {
		return false, err
	}
	if record.Fingerprint != fingerprint {
		record.Fingerprint = fingerprint
		record.Count = 0
	}
	record.Count++
	data, _ = json.Marshal(record)
	f, err := os.CreateTemp(dir, ".idle-*")
	if err != nil {
		return false, err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(data); err != nil {
		f.Close()
		return false, err
	}
	if err = f.Close(); err != nil {
		return false, err
	}
	if err = os.Rename(f.Name(), path); err != nil {
		return false, err
	}
	return record.Count > 2, nil
}
