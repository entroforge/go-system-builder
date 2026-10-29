package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
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
	changed := false
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
	for _, raw := range rows {
		row, _ := raw.(map[string]any)
		_, external, err := packetbody.ReferenceFromIndex(row)
		if err != nil {
			return Snapshot{}, err
		}
		if packetBodySupportedTemplate(row["template_id"]) && !external {
			needsMaintenance = true
		}
	}
	if !needsMaintenance {
		return Snapshot{Revision: revision, State: state}, nil
	}
	return s.applyMutation(revision, Mutation{
		EventID:      fmt.Sprintf("evt-packet-body-maintenance-%s-r%d", stringValue(state["runtime_id"]), revision+1),
		TransitionID: "PACKET-BODY-MAINTENANCE", Event: "packet_body_externalized", Actor: "orchestrator",
		RuntimeID: stringValue(state["runtime_id"]), BaselineGeneration: baselineGeneration(state),
		IdempotencyKey:       fmt.Sprintf("packet-body-maintenance:%s:%d", stringValue(state["runtime_id"]), revision+1),
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

// ClassifyPacketBodyStoragePathsContext recognizes exact immutable framework
// storage objects, including publication interrupted before a Runtime ref.
// Completed objects prove storage format and bytes only, never issuance/reception.
// Exact publisher temporary names identify incomplete framework staging only.
// Unknown neighboring files remain ordinary drift candidates.
func ClassifyPacketBodyStoragePathsContext(ctx context.Context, root string, paths []string) ([]string, error) {
	ctx = WithPacketBodyRoot(ctx, root)
	var controls []string
	for _, path := range paths {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if packetBodyPublicationTemporary(path) {
			if err := checkPacketBodyPublicationTemporary(root, path); err != nil {
				return nil, fmt.Errorf("framework packet publication staging %s: %w", path, err)
			}
			controls = append(controls, path)
			continue
		}
		digest, valid := packetBodyStorageDigest(path)
		if !valid {
			continue
		}
		data, err := workpacket.ReadFile(root, path, packetbody.Limit)
		if err != nil {
			return nil, fmt.Errorf("framework packet storage %s: %w", path, err)
		}
		if packetbody.Digest(data) != digest {
			return nil, fmt.Errorf("framework packet storage %s differs from its content identity", path)
		}
		if err := workpacket.ValidateUniqueJSON(data); err != nil {
			return nil, err
		}
		var row map[string]any
		if err := json.Unmarshal(data, &row); err != nil {
			return nil, err
		}
		if _, present := row["storage_format"]; present {
			return nil, fmt.Errorf("framework packet storage %s is not a complete original packet", path)
		}
		if _, err := canonicalPacketBodyRowContext(ctx, row); err != nil {
			return nil, fmt.Errorf("framework packet storage %s: %w", path, err)
		}
		controls = append(controls, path)
	}
	return controls, ctx.Err()
}

func packetBodyStorageDigest(path string) (string, bool) {
	name := ""
	if strings.HasPrefix(path, packetbody.Directory) {
		name = strings.TrimPrefix(path, packetbody.Directory)
	} else {
		parts := strings.Split(path, "/")
		if len(parts) != 5 || parts[0] != ".claude" || parts[1] != "runtime-checkpoints" || !isCheckpointID(parts[2]) || parts[3] != "packet-bodies" {
			return "", false
		}
		name = parts[4]
	}
	if len(name) != 69 || !strings.HasSuffix(name, ".json") {
		return "", false
	}
	digest := strings.TrimSuffix(name, ".json")
	for _, c := range digest {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return "", false
		}
	}
	return digest, true
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

// This is exactly PublishBounded's private, random temporary filename. It is
// recognized only directly inside the dedicated packet-body directory; a
// neighboring suffix, unknown temporary name, or nested file has no exemption.
func packetBodyPublicationTemporary(path string) bool {
	if !strings.HasPrefix(path, packetbody.Directory) {
		return false
	}
	name := strings.TrimPrefix(path, packetbody.Directory)
	if len(name) != len(".packet-")+32+len(".tmp") || !strings.HasPrefix(name, ".packet-") || !strings.HasSuffix(name, ".tmp") {
		return false
	}
	for _, c := range name[len(".packet-") : len(name)-len(".tmp")] {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

func checkPacketBodyPublicationTemporary(root, path string) error {
	parent, name, err := workpacket.OpenParent(root, path, false)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer parent.Close()
	info, err := parent.Lstat(name)
	// The publisher may have linked the complete body and removed its temp
	// after Git listed it. A vanished staging path cannot be product drift.
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errors.New("packet publication staging is not a regular file")
	}
	return nil
}
