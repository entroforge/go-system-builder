package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/entroforge/go-system-builder/internal/filelock"
	"github.com/entroforge/go-system-builder/internal/integration"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/semantic"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

type relocationIntent struct {
	Checkpoints []relocationCheckpoint       `json:"checkpoints,omitempty"`
	Request     workspace.RebindRequest      `json:"request"`
	Source      runtime.Snapshot             `json:"source"`
	Before      *workspace.ExecutionRegistry `json:"before"`
	After       *workspace.ExecutionRegistry `json:"after"`
}

func runWorkspaceRebind(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("runtime workspace rebind", flag.ContinueOnError)
	fs.SetOutput(stderr)
	root := fs.String("root", ".", "new Main root")
	request := fs.String("request", "", "explicit relocation JSON")
	reason := fs.String("reason", "", "relocation reason")
	if err := parseWorkspaceFlags(fs, args); err != nil {
		return 2
	}
	fail := func(err error) int { fmt.Fprintln(stderr, err); return 1 }
	if *reason == "" {
		return fail(fmt.Errorf("explicit relocation reason is required"))
	}
	if err := workspace.RequireMain(*root); err != nil {
		return fail(err)
	}
	data, err := os.ReadFile(*request)
	if err != nil {
		return fail(err)
	}
	req, err := decodeRelocationRequest(data)
	if err != nil {
		return fail(err)
	}
	actual, err := workspace.Canonical(*root)
	if err != nil || actual != req.NewMainRoot {
		return fail(fmt.Errorf("--root must match requested new Main"))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	ctx, release, err := integration.LockWorkspace(ctx, actual)
	if err != nil {
		return fail(err)
	}
	defer release()
	writer := runtime.NewWriter(filepath.Join(actual, ".claude/loop-state.json"), filepath.Join(actual, ".claude/loop-events.jsonl"), actual, semantic.RuntimeCandidateValidator{})
	receiptPath, err := relocationTransactionPath(filepath.Join(actual, ".claude/evidence/workspace-relocations"), req)
	if err != nil {
		return fail(err)
	}
	var intent relocationIntent
	if old, err := os.ReadFile(receiptPath); err == nil {
		if intent, err = decodeRelocationIntent(old); err != nil {
			return fail(err)
		}
		if !reflect.DeepEqual(intent.Request, req) || intent.Before == nil || intent.After == nil {
			return fail(fmt.Errorf("relocation intent differs from request"))
		}
	} else if !os.IsNotExist(err) {
		return fail(err)
	} else {
		reader := runtime.NewStore(filepath.Join(actual, ".claude/loop-state.json"), filepath.Join(actual, ".claude/loop-events.jsonl"))
		snap, err := reader.Snapshot()
		if err != nil {
			return fail(err)
		}
		b, err := workspace.Decode(snap.State)
		if err != nil {
			return fail(err)
		}
		if b == nil {
			return fail(fmt.Errorf("no binding to relocate"))
		}
		records, err := relocationCheckpoints(actual, b, req)
		if err != nil {
			return fail(err)
		}
		next, err := planTerminalRelocation(ctx, b, snap, req, records)
		if err != nil {
			return fail(err)
		}
		intent = relocationIntent{Request: req, Source: snap, Before: b, After: next, Checkpoints: records}
		if err = os.MkdirAll(filepath.Dir(receiptPath), 0700); err != nil {
			return fail(err)
		}
		payload, _ := json.MarshalIndent(intent, "", "  ")
		// Atomically publish an immutable intent before changing authority.
		f, err := os.CreateTemp(filepath.Dir(receiptPath), ".relocation-*")
		if err != nil {
			return fail(err)
		}
		defer os.Remove(f.Name())
		_, writeErr := f.Write(payload)
		if writeErr == nil {
			writeErr = f.Sync()
		}
		closeErr := f.Close()
		if writeErr != nil {
			return fail(writeErr)
		}
		if closeErr != nil {
			return fail(closeErr)
		}
		if err = os.Link(f.Name(), receiptPath); err != nil {
			return fail(err)
		}

	}
	if err := syncDirectory(filepath.Dir(receiptPath)); err != nil {
		return fail(err)
	}
	if intent.Source.State == nil {
		return fail(fmt.Errorf("relocation receipt lacks source snapshot; preserve it for explicit recovery"))
	}
	// Recheck native Git and the original authority on every retry. The transaction
	// validates the active Runtime fingerprint under its own lock.
	next, err := planTerminalRelocation(ctx, intent.Before, intent.Source, req, intent.Checkpoints)
	if err != nil {
		return fail(err)
	}
	if !reflect.DeepEqual(next, intent.After) {
		return fail(fmt.Errorf("relocation plan changed"))
	}
	for _, e := range next.Executions {
		leaseCtx, stop := context.WithTimeout(ctx, 100*time.Millisecond)
		lease, err := filelock.Acquire(leaseCtx, filepath.Join(actual, ".claude/workspace-launch", e.RuntimeID, fmt.Sprintf("g%d-%s-e%d.lock", e.BaselineGeneration, e.AssignmentID, e.Generation)))
		stop()
		if err != nil {
			return fail(err)
		}
		defer lease()
	}
	if err := recoverRelocatedCheckpoints(actual, intent.Checkpoints, false); err != nil {
		return fail(err)
	}

	snap, recovered := completedRelocationSnapshot(writer, intent)
	if !recovered {
		snap, err = writer.RelocateWorkspace(intent.Source, workspace.Encode(intent.After), "workspace-relocation-"+strings.TrimSuffix(filepath.Base(receiptPath), ".json"), *reason)
	}
	if err != nil {
		return fail(err)
	}
	if err := recoverRelocatedCheckpoints(actual, intent.Checkpoints, true); err != nil {
		return fail(err)
	}
	for _, record := range intent.Checkpoints {
		current, _ := workspace.Decode(snap.State)
		if current.Executions[record.After.AssignmentID].Status != "complete" {
			snap, err = persistExecutionCompletion(actual, snap, record.After.AssignmentID)
			if err != nil {
				return fail(err)
			}
		}
	}
	b, err := workspace.Decode(snap.State)
	if err != nil {
		return fail(err)
	}
	if err = b.Validate(ctx, actual); err != nil {
		return fail(err)
	}
	for _, e := range b.Executions {
		if e.Status == "ready" || e.Status == "preparing" {
			if err = b.RelocatePointer(e, req.OldMainRoot); err != nil {
				return fail(err)
			}
		}
	}
	return encodeJSON(stdout, map[string]any{"binding": b, "relocation_receipt": receiptPath, "revision": snap.Revision})
}

func decodeRelocationRequest(data []byte) (workspace.RebindRequest, error) {
	var req workspace.RebindRequest
	err := json.Unmarshal(data, &req)
	return req, err
}

func decodeRelocationIntent(data []byte) (relocationIntent, error) {
	type plain relocationIntent
	var value plain
	projection := struct {
		*plain
		Request     json.RawMessage `json:"request"`
		Before      json.RawMessage `json:"before"`
		After       json.RawMessage `json:"after"`
		Checkpoints json.RawMessage `json:"checkpoints"`
	}{plain: &value}
	if err := json.Unmarshal(data, &projection); err != nil {
		return relocationIntent{}, err
	}
	req, err := decodeRelocationRequest(projection.Request)
	if err != nil {
		return relocationIntent{}, err
	}
	value.Request = req
	if len(projection.Checkpoints) != 0 && string(projection.Checkpoints) != "null" {
		var rows []struct {
			Path   string          `json:"path"`
			Before json.RawMessage `json:"before"`
			After  json.RawMessage `json:"after"`
		}
		if err := json.Unmarshal(projection.Checkpoints, &rows); err != nil {
			return relocationIntent{}, err
		}
		value.Checkpoints = make([]relocationCheckpoint, 0, len(rows))
		for _, row := range rows {
			var before, after integration.Checkpoint
			if len(row.Before) != 0 {
				before, err = integration.DecodeCheckpoint(row.Before)
				if err != nil {
					return relocationIntent{}, err
				}
			}
			if len(row.After) != 0 {
				after, err = integration.DecodeCheckpoint(row.After)
				if err != nil {
					return relocationIntent{}, err
				}
			}
			value.Checkpoints = append(value.Checkpoints, relocationCheckpoint{Path: row.Path, Before: before, After: after})
		}
	}
	if len(projection.Before) != 0 && string(projection.Before) != "null" {
		before, err := workspace.DecodeRegistry(projection.Before)
		if err != nil {
			return relocationIntent{}, err
		}
		value.Before = &before
	}
	if len(projection.After) != 0 && string(projection.After) != "null" {
		after, err := workspace.DecodeRegistry(projection.After)
		if err != nil {
			return relocationIntent{}, err
		}
		value.After = &after
	}
	return relocationIntent(value), nil
}

// Resolve an existing transaction before allocating a new, non-hash identity.
// The workspace lock serializes publishers and their retries.
func relocationTransactionPath(dir string, req workspace.RebindRequest) (string, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}
	max := 0
	match := ""
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".json") {
			continue
		}
		if strings.HasPrefix(name, "operation-") {
			if n, err := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(name, "operation-"), ".json")); err == nil && n > max {
				max = n
			}
		}
		path := filepath.Join(dir, name)
		data, err := os.ReadFile(path)
		if err != nil {
			return "", err
		}
		intent, err := decodeRelocationIntent(data)
		if err != nil {
			return "", fmt.Errorf("cannot inspect existing relocation transaction %s: %w", path, err)
		}
		if reflect.DeepEqual(intent.Request, req) {
			if match != "" {
				return "", fmt.Errorf("multiple original relocation transactions match the request")
			}
			match = path
		}
	}
	if match != "" {
		return match, nil
	}
	return filepath.Join(dir, fmt.Sprintf("operation-%d.json", max+1)), nil
}
