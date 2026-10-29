package cli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/entroforge/go-system-builder/internal/workstate"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/entroforge/go-system-builder/internal/assignment"
	"github.com/entroforge/go-system-builder/internal/filelock"
	"github.com/entroforge/go-system-builder/internal/fileview"
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
	if bound["id"] != inventory.REQ.ID || bound["path"] != inventory.REQ.Path || bound["version"] != inventory.REQ.Version || bound["sha256"] != inventory.REQ.SHA256 {
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
		inventory.Inputs = append(inventory.Inputs, recovery.InventoryInput{Path: source, Kind: recovery.InputKindCheckpoint, SHA256: sha256Text(data), Size: int64(len(data))})
	}
	for ref, data := range pair.PacketBodies {
		source := filepath.ToSlash(filepath.Dir(relative)) + "/packet-bodies/" + filepath.Base(ref)
		inventory.Inputs = append(inventory.Inputs, recovery.InventoryInput{Path: source, Kind: recovery.InputKindCheckpoint, SHA256: sha256Text(data), Size: int64(len(data))})
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
	id := "rr-" + plan.PlanSHA256[:16]
	dir := ".claude/recovery/" + id
	planPath := filepath.Join(root, filepath.FromSlash(dir+"/plan.json"))
	if existing, err := readRecoveryPlan(planPath); err == nil {
		return existing, filepath.ToSlash(planPath), nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return recoveryPlanDocument{}, "", err
	}
	document := recoveryPlanDocument{Plan: plan, PlanID: id, Status: "planned", CreatedAt: time.Now().UTC().Format(time.RFC3339Nano), CandidateStatePath: dir + "/candidate-state.json", CandidateJournalPath: dir + "/candidate-events.jsonl", CandidateStateSHA256: pair.Manifest.StateSHA256, CandidateJournalSHA256: pair.Manifest.JournalSHA256, CheckpointPath: relative}
	if len(pair.PacketBodies) > 0 {
		document.CandidatePacketBodies = make(map[string]string, len(pair.PacketBodies))
		for ref, data := range pair.PacketBodies {
			sealed := dir + "/packet-bodies/" + filepath.Base(ref)
			existing, publishErr := workpacket.PublishBounded(root, sealed, data, 20<<20)
			if publishErr != nil {
				return document, "", publishErr
			}
			if !bytes.Equal(existing, data) {
				return document, "", invalidRecoveryPlan(fmt.Errorf("immutable recovery packet body differs: %s", ref))
			}
			document.CandidatePacketBodies[ref] = sealed
		}
	}
	document.DocumentSHA256, err = recoveryPlanDocumentHash(document)
	if err != nil {
		return document, "", err
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
		existing, err := workpacket.PublishBounded(root, asset.path, asset.data, asset.limit)
		if err != nil {
			return document, "", err
		}
		if !bytes.Equal(existing, asset.data) {
			return document, "", invalidRecoveryPlan(fmt.Errorf("immutable checkpoint recovery candidate differs"))
		}
	}
	return document, filepath.ToSlash(planPath), nil
}

// The original Git scheduling lock is already held. This guard runs inside
// the original Runtime recovery lock and takes launch leases before applying
// the pair. Marker retries use the same guard with the sealed candidate.
func checkpointRecoveryGuard(ctx context.Context, root string) (func(map[string]any, []byte) error, func()) {
	held := map[string]func(){}
	release := func() {
		for _, unlock := range held {
			unlock()
		}
	}
	guard := func(state map[string]any, journal []byte) error {
		// After the original sealed marker exists its candidate is authoritative.
		// Before that boundary, recheck the latest source inside the Runtime
		// lock so a concurrent Writer cannot make the reviewed plan stale.
		if _, err := os.Stat(filepath.Join(root, ".claude/loop-state.json.recovery-pending.json")); errors.Is(err, os.ErrNotExist) {
			stateBytes, err := json.MarshalIndent(state, "", "  ")
			if err != nil {
				return err
			}
			stateBytes = append(stateBytes, '\n')
			id := workpacket.Digest([]byte(workpacket.Digest(stateBytes) + ":" + workpacket.Digest(journal)))
			latest, err := runtime.LatestRuntimeCheckpoint(root)
			if err != nil || latest != ".claude/runtime-checkpoints/"+id+"/manifest.json" {
				return fmt.Errorf("%w: latest complete checkpoint differs from the sealed candidate", runtime.ErrRecoveryInputDrift)
			}
		} else if err != nil {
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
		pair := runtime.CheckpointPair{State: state, JournalBytes: journal, Manifest: runtime.RuntimeCheckpoint{RuntimeID: fmt.Sprint(state["runtime_id"]), Generation: generation, Revision: revision}}
		if err := runtime.ValidateCheckpointRestore(root, pair); err != nil {
			return fmt.Errorf("%w: %w", runtime.ErrRecoveryInputDrift, err)
		}
		binding, err := checkpointREQWorkspace(state)
		if err != nil || binding == nil {
			return fmt.Errorf("indexed recovery requires original REQ workspace: %v", err)
		}
		files, err := fileview.New(root, "refs/heads/"+binding.DevBranch, []fileview.Rule{{Path: ".", Source: "git_tree"}})
		if err != nil {
			return err
		}
		bound, _ := state["bound_req"].(map[string]any)
		reqPath, _ := bound["path"].(string)
		reqData, err := files.ReadFile(reqPath)
		if err != nil || workpacket.Digest(reqData) != bound["sha256"] {
			return fmt.Errorf("original locked REQ baseline changed")
		}
		registry, err := workspace.Decode(state)
		if err != nil {
			return err
		}
		if registry != nil {
			if err := registry.ValidateRecoveryCheckout(ctx, root, state); err != nil {
				return err
			}
			ids := make([]string, 0, len(registry.Executions))
			for id := range registry.Executions {
				ids = append(ids, id)
			}
			sort.Strings(ids)
			for _, id := range ids {
				e := registry.Executions[id]
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
				if err := registry.ValidateInputs(e); err != nil {
					return err
				}
				if e.Status == "ready" {
					if err := registry.ValidateExecution(ctx, e, false); err != nil {
						return err
					}
					if err := registry.ValidateBootstrap(e); err != nil {
						return err
					}
					if e.Purpose == workspace.DocumentReviewPurpose {
						p, err := workspace.DocumentReviewPacket(state, e)
						if err != nil {
							return err
						}
						if err := registry.ValidateDocumentReviewView(e, p); err != nil {
							return err
						}
					}
				}
			}
		}
		entities, _ := state["entities"].(map[string]any)
		life, _ := state["lifecycle"].(map[string]any)
		entry, _ := recoveryInteger(life["phase_revision"])
		packets, _ := entities["work_packets"].([]any)
		for _, raw := range packets {
			row, _ := raw.(map[string]any)
			switch row["template_id"] {
			case "s7-review-result-v1", "s8-hypothesis-result-v1", "s9-plan-report-v1", "s9-repair-result-v1":
				// ApplyRecovery has already checked these exact typed packets,
				// original issuance and receipts in ValidateCheckpointIndices.
				// This loop adds live source checks for legacy planning/S5;
				// those packets cannot be decoded as a generic S5 Packet.
				continue
			}
			if row["template_id"] == "s4-task-planning-v1" {
				p, ok, err := runtime.FindTaskPlanningPacket(state, fmt.Sprint(row["packet_id"]))
				if err != nil || !ok {
					return fmt.Errorf("recovery S4 packet cannot be resolved")
				}
				if life["state"] == "planning" && life["phase"] == "tasks" && p.Binding.Generation == generation && p.Binding.StageEntry == entry {
					if err := runtime.ValidateTaskPlanningPacket(root, state, p, files); err != nil {
						return err
					}
				}
				continue
			}
			if row["template_id"] == "s3-contract-v1" {
				p, ok, err := runtime.FindContractPacket(state, fmt.Sprint(row["packet_id"]))
				if err != nil || !ok {
					return fmt.Errorf("recovery S3 packet cannot be resolved")
				}
				if life["state"] == "planning" && life["phase"] == "contracts" && p.Binding.Generation == generation && p.Binding.StageEntry == entry {
					if err := runtime.ValidateIssuedContractPacket(root, state, p, files); err != nil {
						return err
					}
				}
				continue
			}
			if row["template_id"] == "s2-design-v1" {
				p, ok, err := runtime.FindDesignPacket(state, fmt.Sprint(row["packet_id"]))
				if err != nil || !ok {
					return fmt.Errorf("recovery S2 packet cannot be resolved")
				}
				if life["state"] == "planning" && life["phase"] == "design" && p.Binding.Generation == generation && p.Binding.StageEntry == entry {
					if err := runtime.ValidateDesignPacket(root, state, p, files); err != nil {
						return err
					}
				}
				continue
			}
			p, ok, err := runtime.FindWorkPacket(state, fmt.Sprint(row["packet_id"]))
			if err != nil || !ok {
				return fmt.Errorf("recovery packet cannot be resolved")
			}
			if life["state"] != "document_verification" || p.Binding.Generation != generation || p.Binding.StageEntry != entry {
				continue
			}
			if r, received, err := runtime.FindSubmission(state, p.ID); err != nil {
				return err
			} else if received && r.RevisionPacketID != "" {
				continue
			}
			if err := runtime.ValidateWorkPacket(root, state, p, files); err != nil {
				return err
			}
		}
		agents, _ := entities["agents"].([]any)
		for _, raw := range agents {
			a, _ := raw.(map[string]any)
			if workstate.AgentActivity(a) == "working" {
				if err := assignment.ValidateWorkingAuthorization(root, state, fmt.Sprint(a["id"])); err != nil {
					return err
				}
			}
		}
		return files.Verify()
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
	// Frozen candidates may survive source retention cleanup, but any extant
	// original bytes must still match the reviewed inventory.
	for _, input := range document.Inputs {
		if input.Kind != recovery.InputKindCheckpoint {
			continue
		}
		data, err := workpacket.ReadFile(root, input.Path, 64<<20)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil || sha256Text(data) != input.SHA256 {
			return fmt.Errorf("%w: checkpoint input changed: %s", runtime.ErrRecoveryInputDrift, input.Path)
		}
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
	if binding.ProjectRoot == "" || binding.DevBranch == "" || binding.ReleaseUpstream == "" || binding.BoundCommit == "" {
		return nil, fmt.Errorf("original REQ workspace binding is incomplete")
	}
	return &binding, nil
}
func sha256Text(data []byte) string { return fmt.Sprintf("%x", sha256.Sum256(data)) }
