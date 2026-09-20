package metrics

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/entroforge/go-system-builder/internal/filelock"
)

type countedObservation struct {
	Observation observation `json:"observation"`
	Count       int64       `json:"count"`
}
type observationSummary struct {
	Version  int                           `json:"version"`
	Totals   map[string]countedObservation `json:"totals"`
	Consumed map[string]bool               `json:"consumed"`
}

// CompactObservations preserves lifetime counters while removing consumed
// spool files. It is maintenance, never part of a mutating Hook's hot path.
func CompactObservations(root string) error {
	snap := emptySnapshot()
	return NewStore(root).scanObservations(&snap, true)
}

func applyObservation(s *Snapshot, o observation, count int64) error {
	switch o.Kind {
	case "gate":
		s.GateEvaluations[o.Label] += count
	case "transition":
		s.TransitionCommits[o.Label] += count
	case "cas":
		s.CASConflicts += count
	case "milestone":
		s.MilestoneRefreshFailures += count
		s.MilestoneRefreshFailureReasons[o.Label] += count
	case "recovery":
		s.RecoveryPackets += count
	case "integration":
		v := s.IntegrationDuration[o.Label]
		v.Count += count
		v.SumMS += o.Duration
		s.IntegrationDuration[o.Label] = v
	default:
		return fmt.Errorf("unknown metrics observation %q", o.Kind)
	}
	return nil
}

func (s *Store) scanObservations(snap *Snapshot, force bool) error {
	dir := filepath.Join(s.root, ".claude/hook-metrics")
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	release, err := filelock.Acquire(ctx, filepath.Join(dir, "compact.lock"))
	if err != nil {
		return err
	}
	defer release()
	summary := observationSummary{Version: 1, Totals: map[string]countedObservation{}, Consumed: map[string]bool{}}
	data, err := os.ReadFile(filepath.Join(dir, "summary.json"))
	if err == nil {
		if err = json.Unmarshal(data, &summary); err != nil {
			return err
		}
		if summary.Version != 1 || summary.Totals == nil || summary.Consumed == nil {
			return fmt.Errorf("invalid observation summary")
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	pending := make(map[string]observation)
	for _, entry := range entries {
		name := entry.Name()
		if (strings.HasPrefix(name, ".pending-") && !strings.HasSuffix(name, ".json")) || strings.HasPrefix(name, ".compact-") {
			info, err := entry.Info()
			if os.IsNotExist(err) {
				continue
			} // producer atomically published this pending file
			if err != nil {
				return err
			}
			if info.Mode().IsRegular() && time.Since(info.ModTime()) > 24*time.Hour {
				if err = os.Remove(filepath.Join(dir, name)); err != nil && !os.IsNotExist(err) {
					return err
				}
			}
			continue
		}
		if name == "summary.json" || !strings.HasSuffix(name, ".json") || summary.Consumed[name] {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return err
		}
		var o observation
		if err = json.Unmarshal(data, &o); err != nil {
			return fmt.Errorf("metrics observation %s: %w", name, err)
		}
		if err = applyObservation(snap, o, 1); err != nil {
			return err
		}
		pending[name] = o
	}
	for _, v := range summary.Totals {
		if err = applyObservation(snap, v.Observation, v.Count); err != nil {
			return err
		}
	}
	if !force && len(pending) < 256 && len(summary.Consumed) == 0 {
		return nil
	}
	for name, o := range pending {
		keyBytes, _ := json.Marshal([]string{o.Kind, o.Label})
		key := string(keyBytes)
		v := summary.Totals[key]
		v.Observation.Kind, v.Observation.Label = o.Kind, o.Label
		v.Observation.Duration += o.Duration
		v.Count++
		summary.Totals[key] = v
		summary.Consumed[name] = true
	}
	// Commit totals and their source identities together before deleting any file.
	if err = writeObservationSummary(dir, summary); err != nil {
		return err
	}
	for name := range summary.Consumed {
		if filepath.Base(name) != name || !strings.HasPrefix(name, ".pending-") || !strings.HasSuffix(name, ".json") {
			return fmt.Errorf("invalid consumed observation name")
		}
		if err = os.Remove(filepath.Join(dir, name)); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	summary.Consumed = map[string]bool{}
	return writeObservationSummary(dir, summary)
}

func writeObservationSummary(dir string, summary observationSummary) error {
	f, err := os.CreateTemp(dir, ".compact-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = json.NewEncoder(f).Encode(summary); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(f.Name(), filepath.Join(dir, "summary.json"))
}
