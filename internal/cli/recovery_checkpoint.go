package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/entroforge/go-system-builder/internal/filelock"
	"github.com/entroforge/go-system-builder/internal/packetbody"
	"github.com/entroforge/go-system-builder/internal/recovery"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/semantic"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

func persistCheckpointRecoveryPlan(root string, inventory recovery.Inventory, path string) (recoveryPlanDocument, string, error) {
	full, err := resolveRecoveryPath(root, path)
	if err != nil {
		return recoveryPlanDocument{}, "", err
	}
	relative, err := filepath.Rel(root, full)
	if err != nil {
		return recoveryPlanDocument{}, "", err
	}
	relative = filepath.ToSlash(relative)
	pair, err := runtime.ReadRuntimeCheckpoint(root, relative, semantic.RuntimeCandidateValidator{})
	if err != nil {
		return recoveryPlanDocument{}, "", invalidRecoveryPlan(err)
	}
	bound, _ := pair.State["bound_req"].(map[string]any)
	if bound["id"] != inventory.REQ.ID || bound["path"] != inventory.REQ.Path || bound["version"] != inventory.REQ.Version {
		return recoveryPlanDocument{}, "", fmt.Errorf("%w: checkpoint REQ differs from selected locked source", runtime.ErrRecoveryInputDrift)
	}
	actual, err := checkpointREQWorkspace(pair.State)
	if err != nil {
		return recoveryPlanDocument{}, "", err
	}
	if inventory.REQ.Workspace == nil || actual.DevBranch != inventory.REQ.Workspace.DevBranch || actual.ReleaseUpstream != inventory.REQ.Workspace.ReleaseUpstream || actual.ProjectRoot != inventory.REQ.Workspace.ProjectRoot {
		return recoveryPlanDocument{}, "", fmt.Errorf("%w: checkpoint workspace differs from explicit binding", runtime.ErrRecoveryInputDrift)
	}
	// The review inventories the exact original source, while the candidate is
	// frozen in the plan directory and never rebuilt from business documents.
	for _, source := range []string{relative, pair.Manifest.StatePath, pair.Manifest.JournalPath} {
		data, err := workpacket.ReadFile(root, source, 64<<20)
		if err != nil {
			return recoveryPlanDocument{}, "", err
		}
		inventory.Inputs = append(inventory.Inputs, recovery.InventoryInput{Path: source, Kind: recovery.InputKindCheckpoint, Size: int64(len(data))})
	}
	for ref, data := range pair.PacketBodies {
		source := runtime.CheckpointPacketBodyPath(filepath.ToSlash(filepath.Dir(relative)), packetbody.Reference{Path: ref})
		inventory.Inputs = append(inventory.Inputs, recovery.InventoryInput{Path: source, Kind: recovery.InputKindCheckpoint, Size: int64(len(data))})
	}
	life, _ := pair.State["lifecycle"].(map[string]any)
	cursor := fmt.Sprint(life["state"])
	if phase, ok := life["phase"].(string); ok && phase != "" {
		cursor += "." + phase
	}
	plan, err := recovery.BuildPlanForCursor(inventory, cursor, recovery.PlanConfidenceOriginalPair)
	if err != nil {
		return recoveryPlanDocument{}, "", err
	}
	base := filepath.Join(root, ".claude", "recovery")
	if err := os.MkdirAll(base, 0o700); err != nil {
		return recoveryPlanDocument{}, "", err
	}
	var id, dir, planDirectory string
	for {
		id, err = newRecoveryPlanID()
		if err != nil {
			return recoveryPlanDocument{}, "", err
		}
		dir = ".claude/recovery/" + id
		planDirectory = filepath.Join(base, id)
		if err = os.Mkdir(planDirectory, 0o700); errors.Is(err, os.ErrExist) {
			continue
		}
		if err != nil {
			return recoveryPlanDocument{}, "", err
		}
		break
	}
	planPath := filepath.Join(planDirectory, "plan.json")
	document := recoveryPlanDocument{Plan: plan, PlanID: id, Status: "planned", CreatedAt: time.Now().UTC().Format(time.RFC3339Nano), CandidateStatePath: dir + "/candidate-state.json", CandidateJournalPath: dir + "/candidate-events.jsonl", CheckpointPath: relative}
	if len(pair.PacketBodies) > 0 {
		document.CandidatePacketBodies = make(map[string]string, len(pair.PacketBodies))
		index := 0
		for ref, data := range pair.PacketBodies {
			sealed := fmt.Sprintf("%s/original-bodies/body-%d.json", dir, index)
			index++
			created, err := workpacket.PublishNewBounded(root, sealed, data, 20<<20)
			if err != nil {
				return document, "", err
			}
			if !created {
				return document, "", invalidRecoveryPlan(fmt.Errorf("original recovery body path became occupied"))
			}
			document.CandidatePacketBodies[ref] = sealed
		}
	}
	planBytes, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return document, "", err
	}
	planBytes = append(planBytes, '\n')
	for _, asset := range []struct {
		path  string
		data  []byte
		limit int64
	}{{document.CandidateStatePath, pair.StateBytes, 16 << 20}, {document.CandidateJournalPath, pair.JournalBytes, 64 << 20}, {dir + "/plan.json", planBytes, 4 << 20}} {
		created, err := workpacket.PublishNewBounded(root, asset.path, asset.data, asset.limit)
		if err != nil {
			return document, "", err
		}
		if !created {
			return document, "", invalidRecoveryPlan(fmt.Errorf("recovery plan asset path became occupied"))
		}
	}
	return document, filepath.ToSlash(planPath), nil
}

// The original Git scheduling lock is already held. This guard runs inside
// the original Runtime recovery lock and takes launch leases before applying
// the pair. Marker retries use the same guard with the sealed candidate.
func checkpointRecoveryGuard(ctx context.Context, root string, checkpointSource *string) (func(map[string]any, []byte) error, func()) {
	held := map[string]func(){}
	release := func() {
		for _, unlock := range held {
			unlock()
		}
	}
	guard := func(state map[string]any, journal []byte) error {
		var original *runtime.CheckpointPair
		// After the original sealed marker exists its candidate is authoritative.
		// Before that boundary, recheck the latest source inside the Runtime
		// lock so a concurrent Writer cannot make the reviewed plan stale.
		if _, err := os.Stat(filepath.Join(root, ".claude/loop-state.json.recovery-pending.json")); errors.Is(err, os.ErrNotExist) && checkpointSource != nil && *checkpointSource != "" {
			latest, err := runtime.LatestRuntimeCheckpoint(root)
			if err != nil {
				return fmt.Errorf("%w: latest checkpoint unavailable: %v", runtime.ErrRecoveryInputDrift, err)
			}
			pair, err := runtime.ReadRuntimeCheckpoint(root, latest, semantic.RuntimeCandidateValidator{})
			if err != nil {
				return err
			}
			candidateJournal, _ := state["journal"].(map[string]any)
			sourceJournal, _ := pair.State["journal"].(map[string]any)
			candidateBaseline, _ := state["baseline"].(map[string]any)
			sourceBaseline, _ := pair.State["baseline"].(map[string]any)
			if state["runtime_id"] != pair.State["runtime_id"] || state["revision"] != pair.State["revision"] || candidateBaseline["generation"] != sourceBaseline["generation"] || candidateJournal["last_sequence"] != sourceJournal["last_sequence"] || candidateJournal["last_event_id"] != sourceJournal["last_event_id"] {
				return fmt.Errorf("%w: latest checkpoint responsibility changed", runtime.ErrRecoveryInputDrift)
			}
			original = &pair
		} else if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err := workspace.RequireMain(root); err != nil {
			return err
		}
		baseline, _ := state["baseline"].(map[string]any)
		generation, err := recoveryInteger(baseline["generation"])
		if err != nil {
			return err
		}
		revision, err := recoveryInteger(state["revision"])
		if err != nil {
			return err
		}
		if _, err := os.Stat(filepath.Join(root, ".claude/loop-state.json.recovery-pending.json")); errors.Is(err, os.ErrNotExist) {
			pair := runtime.CheckpointPair{State: state, JournalBytes: journal, Manifest: runtime.RuntimeCheckpoint{RuntimeID: fmt.Sprint(state["runtime_id"]), Generation: generation, Revision: revision}}
			if original != nil {
				pair = *original
			}
			if err := runtime.ValidateCheckpointRestore(root, pair); err != nil {
				return fmt.Errorf("%w: %w", runtime.ErrRecoveryInputDrift, err)
			}
		} else if err != nil {
			return err
		}
		var registry *workspace.ExecutionRegistry
		if raw := state["workspace"]; raw != nil {
			data, err := json.Marshal(raw)
			if err != nil {
				return err
			}
			decoded, err := workspace.DecodeRegistry(data)
			if err != nil {
				return fmt.Errorf("read original Worker responsibilities: %w", err)
			}
			registry = &decoded
		}
		if registry != nil {
			ids := make([]string, 0, len(registry.Executions))
			for id := range registry.Executions {
				ids = append(ids, id)
			}
			sort.Strings(ids)
			for _, id := range ids {
				e := registry.Executions[id]
				if (e.Status != "ready" && e.Status != "preparing") || e.RuntimeID != fmt.Sprint(state["runtime_id"]) || e.BaselineGeneration != generation {
					continue
				}
				leasePath := filepath.Join(root, ".claude/workspace-launch", e.RuntimeID, fmt.Sprintf("g%d-%s-e%d.lock", e.BaselineGeneration, e.AssignmentID, e.Generation))
				if held[leasePath] == nil {
					budget, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
					unlock, err := filelock.Acquire(budget, leasePath)
					cancel()
					if err != nil {
						return fmt.Errorf("recovery Worker still owns its launch lease: %w", err)
					}
					held[leasePath] = unlock
				}

			}
		}
		return nil
	}
	return guard, release
}

// verifyCheckpointPlanSource binds the reviewed candidate to the latest source.
// A later checkpoint invalidates this plan rather than rolling work backward.
func verifyCheckpointPlanSource(root string, document recoveryPlanDocument) error {
	if document.Confidence != recovery.PlanConfidenceOriginalPair {
		if document.CheckpointPath != "" {
			return invalidRecoveryPlan(fmt.Errorf("checkpoint source on an artifact reconstruction plan"))
		}
		return nil
	}
	if document.CheckpointPath == "" {
		return invalidRecoveryPlan(fmt.Errorf("checkpoint restoration has no original source"))
	}
	latest, err := runtime.LatestRuntimeCheckpoint(root)
	if err != nil || latest != document.CheckpointPath {
		return fmt.Errorf("%w: original checkpoint is no longer latest: %v", runtime.ErrRecoveryInputDrift, err)
	}

	return nil
}

func checkpointREQWorkspace(state map[string]any) (*workspace.Binding, error) {
	bound, _ := state["bound_req"].(map[string]any)
	data, err := json.Marshal(bound["workspace"])
	if err != nil {
		return nil, err
	}
	var binding workspace.Binding
	if err := json.Unmarshal(data, &binding); err != nil {
		return nil, err
	}
	if binding.ProjectRoot == "" || binding.DevBranch == "" || binding.ReleaseUpstream == "" {
		return nil, fmt.Errorf("original REQ workspace binding is incomplete")
	}
	return &binding, nil
}
