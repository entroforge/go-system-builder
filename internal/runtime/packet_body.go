package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/entroforge/go-system-builder/internal/packetbody"
	"github.com/entroforge/go-system-builder/internal/schema"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

// WithPacketBodyRoot supplies the original authority repository for selected
// immutable packet reads; it does not hydrate a Snapshot or confer authority.
func WithPacketBodyRoot(ctx context.Context, root string) context.Context {
	return packetbody.WithRoot(ctx, root)
}

// Only newly appended supported S7/S9 rows are externalized. Legacy rows, pending markers
// and checkpoint bytes retain their exact physical representation. Other
// stages remain inline until all of their original consumers support bodies.
func (s *Store) externalizeNewPacketBodies(previous, candidate map[string]any) (bool, error) {
	if s.root == "" || s.offlineRecovery {
		return false, nil
	}
	before, _ := previous["entities"].(map[string]any)
	oldRows, _ := before["work_packets"].([]any)
	oldIDs := make(map[string]bool, len(oldRows))
	for _, raw := range oldRows {
		if row, ok := raw.(map[string]any); ok {
			oldIDs[stringValue(row["packet_id"])] = true
		}
	}
	entities, _ := candidate["entities"].(map[string]any)
	rows, _ := entities["work_packets"].([]any)
	changed := preserveReviewResultReceptions(previous, candidate)
	for i, raw := range rows {
		row, ok := raw.(map[string]any)
		if !ok || !packetBodySupportedTemplate(row["template_id"]) || oldIDs[stringValue(row["packet_id"])] {
			continue
		}
		_, external, err := packetbody.ReferenceFromIndex(row)
		if err != nil {
			return false, err
		}
		if external {
			if _, err := packetbody.Read(WithPacketBodyRoot(s.Context(), s.root), row); err != nil {
				return false, err
			}
			continue
		}
		if _, err := canonicalPacketBodyRowContext(WithPacketBodyRoot(s.Context(), s.root), row); err != nil {
			return false, fmt.Errorf("new original packet body: %w", err)
		}
		index, err := packetbody.Publish(s.Context(), s.root, row)
		if err != nil {
			return false, err
		}
		rows[i] = index
		changed = true
	}
	return changed, nil
}

func (s *Store) validatePacketBodyIndexRepresentation(state map[string]any) error {
	if err := s.Context().Err(); err != nil {
		return err
	}
	encoded, err := json.Marshal(state)
	if err != nil {
		return err
	}
	return schema.NewEmbeddedValidator().ValidateBytes("loop-state.schema.json", encoded)
}

// ExternalizeReviewResultPacketBodies is explicit framework maintenance. It
// completes any original pending transaction first, then commits only the
// physical packet representation change under the original Runtime Writer. It is
// neither a new issuance nor a professional reception.
func (s *Store) ExternalizeReviewResultPacketBodies() (Snapshot, error) {
	return s.ExternalizePacketBodies()
}

// ExternalizePacketBodies maintains all supported S7 and S9 packet subtypes.
func (s *Store) ExternalizePacketBodies() (Snapshot, error) {
	if !s.mutationCapable || s.root == "" || s.offlineRecovery {
		return Snapshot{}, ErrCandidateValidatorRequired
	}
	release, err := s.acquireLock(s.statePath+".lock", 5*time.Second)
	if err != nil {
		return Snapshot{}, err
	}
	defer release()
	if err := s.recoverPendingWritesLocked(); err != nil {
		return Snapshot{}, err
	}
	state, err := s.read()
	if err != nil {
		return Snapshot{}, err
	}
	revision, err := integerField(state, "revision")
	if err != nil {
		return Snapshot{}, err
	}
	entities, _ := state["entities"].(map[string]any)
	rows, _ := entities["work_packets"].([]any)
	needsMaintenance := false
	maintainedPacket := ""
	for _, raw := range rows {
		row, _ := raw.(map[string]any)
		_, external, err := packetbody.ReferenceFromIndex(row)
		if err != nil {
			return Snapshot{}, err
		}
		if packetBodySupportedTemplate(row["template_id"]) && !external {
			needsMaintenance = true
			if maintainedPacket == "" {
				maintainedPacket = stringValue(row["packet_id"])
			}
		}
	}
	if !needsMaintenance {
		return Snapshot{Revision: revision, State: state}, nil
	}
	// G18: the maintenance is named by the runtime it repairs and by the
	// legacy packet body it externalizes. A later maintenance of a different
	// legacy body is a different intent and therefore keeps a different
	// identity, while a replay of the same maintenance keeps one identity.
	runtimeID := stringValue(state["runtime_id"])
	return s.applyMutation(revision, Mutation{
		EventID:      fmt.Sprintf("evt-packet-body-maintenance-%s-%s", runtimeID, maintainedPacket),
		TransitionID: "PACKET-BODY-MAINTENANCE", Event: "packet_body_externalized", Actor: "orchestrator",
		RuntimeID: runtimeID, BaselineGeneration: baselineGeneration(state),
		IdempotencyKey:       fmt.Sprintf("packet-body-maintenance:%s:%s", runtimeID, maintainedPacket),
		RetainLastTransition: true,
		Apply: func(candidate map[string]any) error {
			ctx := WithReviewResultReadCache(WithPacketBodyRoot(s.Context(), s.root))
			indexed, err := packetBodyMaintenanceRows(ctx, candidate)
			if err != nil {
				return err
			}
			replacements := map[string]map[string]any{}
			for _, row := range indexed {
				_, external, err := packetbody.ReferenceFromIndex(row.row)
				if err != nil {
					return err
				}
				if external {
					continue
				}
				p, err := canonicalPacketBodyRowContext(ctx, row.row)
				if err != nil {
					return err
				}
				if err := verifyPacketBodyIssueContext(ctx, s.root, candidate, p); err != nil {
					return err
				}
				// Validate or restore the original subtype's delivery view;
				// private author answers are never opened or overwritten.
				if err := maintainOriginalPacketView(ctx, s.root, row.row, p); err != nil {
					return err
				}
				index, err := packetbody.Publish(ctx, s.root, row.row)
				if err != nil {
					return err
				}
				replacements[stringValue(row.row["packet_id"])] = index
			}
			entities, _ := candidate["entities"].(map[string]any)
			rows, _ := entities["work_packets"].([]any)
			for i, raw := range rows {
				row, _ := raw.(map[string]any)
				if replacement := replacements[stringValue(row["packet_id"])]; replacement != nil {
					rows[i] = replacement
				}
			}
			return nil
		},
	})
}

func validatePacketBodyFormats(state map[string]any) error {
	entities, _ := state["entities"].(map[string]any)
	rows, _ := entities["work_packets"].([]any)
	for _, raw := range rows {
		row, _ := raw.(map[string]any)
		_, external, err := packetbody.ReferenceFromIndex(row)
		if err != nil {
			return err
		}
		if external && !packetBodySupportedTemplate(row["template_id"]) {
			return fmt.Errorf("unsupported packet body subtype %q", row["template_id"])
		}
		if external {
			if err := validatePacketBodyIndexRow(row); err != nil {
				return err
			}
		}
	}
	return nil
}

func packetBodySupportedTemplate(template any) bool {
	return template == reviewResultPacketTemplateID || template == repairPlanReportPacketTemplateID || template == repairResultPacketTemplateID
}

// Pending recovery checks immutable referenced bytes without changing the
// pending state's physical representation or re-consuming private answers.
func verifyReferencedPacketBodies(ctx context.Context, root string, state map[string]any) error {
	entities, _ := state["entities"].(map[string]any)
	rows, _ := entities["work_packets"].([]any)
	ctx = WithPacketBodyRoot(ctx, root)
	for _, raw := range rows {
		row, _ := raw.(map[string]any)
		_, external, err := packetbody.ReferenceFromIndex(row)
		if err != nil {
			return err
		}
		if !external {
			continue
		}
		if _, err := packetbody.Read(ctx, row); err != nil {
			return err
		}
	}
	return ctx.Err()
}

func canonicalPacketBodyRowContext(ctx context.Context, row map[string]any) (any, error) {
	switch row["template_id"] {
	case reviewResultPacketTemplateID:
		return canonicalReviewResultRowContext(ctx, row)
	case repairPlanReportPacketTemplateID:
		return canonicalRepairPlanReportRowContext(ctx, row)
	case repairResultPacketTemplateID:
		return canonicalRepairResultRowContext(ctx, row)
	default:
		return nil, fmt.Errorf("unsupported original packet body subtype %q", row["template_id"])
	}
}
func verifyPacketBodyIssueContext(ctx context.Context, root string, state map[string]any, p any) error {
	switch packet := p.(type) {
	case workpacket.ReviewResultPacket:
		return VerifyReviewResultPacketIssueContext(ctx, root, state, packet)
	case workpacket.RepairPlanReportPacket:
		return VerifyRepairPlanReportPacketIssueContext(ctx, root, state, packet)
	case workpacket.RepairResultPacket:
		return VerifyRepairResultPacketIssueContext(ctx, root, state, packet)
	default:
		return errors.New("unsupported original packet body identity")
	}
}
func packetBodyMaintenanceRows(ctx context.Context, state map[string]any) ([]reviewResultIndexedRow, error) {
	// Reuse the global ID/structure checks, including cross-subtype duplicates.
	if _, err := reviewResultRowsContext(ctx, state); err != nil {
		return nil, err
	}
	entities, _ := state["entities"].(map[string]any)
	rows, _ := entities["work_packets"].([]any)
	var selected []reviewResultIndexedRow
	for _, raw := range rows {
		row, _ := raw.(map[string]any)
		if packetBodySupportedTemplate(row["template_id"]) {
			selected = append(selected, reviewResultIndexedRow{row: row})
		}
	}
	return selected, nil
}

// ClassifyPacketBodyStoragePathsContext recognizes only paths named by the
// existing Runtime, a coherent pending commit, or an indexed checkpoint. A
// path, filename or parseable body alone cannot establish issuance or confer
// authority to delete an unregistered file. Unknown paths are left untouched.
func ClassifyPacketBodyStoragePathsContext(ctx context.Context, root string, paths []string) ([]string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	wanted := map[string]bool{}
	needCurrent, needCheckpoint := false, false
	for _, path := range paths {
		if packetbody.IsBodyPath(path) {
			wanted[path], needCurrent = true, true
		} else if strings.HasPrefix(path, ".claude/runtime-checkpoints/") && strings.Contains(path, "/packet-bodies/") {
			wanted[path], needCheckpoint = true, true
		}
	}
	if len(wanted) == 0 {
		return nil, nil
	}
	referenced := map[string]bool{}
	if needCurrent {
		if state := readPacketClassificationState(root, ".claude/loop-state.json", ""); state != nil {
			collectPacketClassificationPaths(state, "", wanted, referenced)
		}
		// A pending commit becomes a durable original Writer fact only after
		// the marker is complete and its state/event coordinates agree.
		if state := readCoherentPacketPendingState(root); state != nil {
			collectPacketClassificationPaths(state, "", wanted, referenced)
		}
	}
	if needCheckpoint {
		if index, err := readCheckpointIndex(root); err == nil {
			for _, manifest := range index.Manifests {
				dir := strings.TrimSuffix(manifest, "/manifest.json")
				interested := false
				for path := range wanted {
					if strings.HasPrefix(path, dir+"/packet-bodies/") {
						interested = true
						break
					}
				}
				if !interested {
					continue
				}
				data, err := workpacket.ReadFile(root, manifest, 4096)
				var record map[string]any
				if err != nil || json.Unmarshal(data, &record) != nil || record["schema_version"] != "1.0.0" || stringValue(record["state_path"]) != dir+"/state.json" {
					continue
				}
				if state := readPacketClassificationState(root, stringValue(record["state_path"]), ""); state != nil {
					collectPacketClassificationPaths(state, dir, wanted, referenced)
				}
			}
		}
	}
	var controls []string
	for _, path := range paths {
		if referenced[path] {
			controls = append(controls, path)
		}
	}
	return controls, ctx.Err()
}

func readPacketClassificationState(root, path, field string) map[string]any {
	data, err := workpacket.ReadFile(root, path, checkpointStateLimit)
	if err != nil {
		return nil
	}
	var state map[string]any
	if json.Unmarshal(data, &state) != nil {
		return nil
	}
	if field != "" {
		state, _ = state[field].(map[string]any)
	}
	return state
}

func readCoherentPacketPendingState(root string) map[string]any {
	data, err := workpacket.ReadFile(root, ".claude/loop-state.json.commit-pending.json", checkpointStateLimit)
	if err != nil {
		return nil
	}
	var raw map[string]any
	if json.Unmarshal(data, &raw) != nil {
		return nil
	}
	state, _ := raw["state"].(map[string]any)
	event, _ := raw["journal_event"].(map[string]any)
	marker := commitPending{State: state, JournalEvent: event, RequestID: stringValue(raw["request_id"]), IdempotencyKey: stringValue(raw["idempotency_key"]), RetainLastTransition: raw["retain_last_transition"] == true}
	if raw["schema_version"] != "1.0.0" || validatePendingCommitCoherence(marker) != nil {
		return nil
	}
	return state
}

func collectPacketClassificationPaths(state map[string]any, checkpointDir string, wanted, result map[string]bool) {
	entities, _ := state["entities"].(map[string]any)
	rows, _ := entities["work_packets"].([]any)
	for _, raw := range rows {
		row, _ := raw.(map[string]any)
		ref, external, err := packetbody.ReferenceFromIndex(row)
		if err != nil || !external || stringValue(row["packet_id"]) == "" || !packetBodySupportedTemplate(row["template_id"]) {
			continue
		}
		path := ref.Path
		if checkpointDir != "" {
			path = checkpointBodyPath(checkpointDir, ref)
		}
		if wanted[path] {
			result[path] = true
		}
	}
}

func validatePacketBodyIndexRow(row map[string]any) error {
	_, external, err := packetbody.ReferenceFromIndex(row)
	if err != nil || !external {
		return err
	}
	asset := ""
	switch row["template_id"] {
	case reviewResultPacketTemplateID:
		asset = "review-result-packet-index.schema.json"
	case repairPlanReportPacketTemplateID:
		asset = "repair-plan-report-packet-index.schema.json"
	case repairResultPacketTemplateID:
		asset = "repair-result-packet-index.schema.json"
	default:
		return fmt.Errorf("unsupported packet body subtype %q", row["template_id"])
	}
	data, err := json.Marshal(row)
	if err != nil {
		return err
	}
	if err := schema.NewEmbeddedValidator().ValidateBytes(asset, data); err != nil {
		return fmt.Errorf("packet body index %s: %w", stringValue(row["packet_id"]), err)
	}
	return nil
}
