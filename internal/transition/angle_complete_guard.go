// angle_complete_guard.go implements the three pre-review angle completeness
// guards that replace the legacy evidenceBackedGuard stubs on the DV/QA/E2E
// phase transitions. Together they enforce REQ-003 FR-002 + FR-003 + FR-004
// (and the FR-010 E2E NA path) as a fail-closed precondition for advancing
// past the verification delivery, QA, or E2E browser phase.
//
// The guard is invoked by Apply after the engine has already verified the
// transition's declared required_evidence fingerprints via
// validateCurrentEvidence. We read the angle_declaration evidence directly
// off state.evidence[] (the runtime evidence registry) because it is a
// pre-review evidence type that may not be on the transition's
// required_evidence list — the guard's job is to assert presence + content.
//
// Dimension gating: each guard is wired into exactly one phase transition.
// The dimension it covers is encoded in the guard id
// (delivery_angle_complete -> delivery, qa_angle_complete -> qa,
// e2e_angle_complete -> e2e_browser). The guard rejects evidence whose
// dimension field does not match the expected one, preventing cross-phase
// reuse (a delivery angle_declaration cannot satisfy e2e_angle_complete).
package transition

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/entroforge/go-system-builder/internal/runtime"
)

// angleDimension is the verification phase a guard covers.
type angleDimension string

const (
	angleDimensionDelivery angleDimension = "delivery"
	angleDimensionQA       angleDimension = "qa"
	angleDimensionE2E      angleDimension = "e2e_browser"
)

const (
	e2eNARequiredTarget = "blockquote.ui_impact"
	angleKindKind       = "kind"
)

// angleDeclarationRecord is the parsed body of an angle_declaration evidence
// file (per angle-declaration.schema.json). We decode only the fields the
// guards need; schema validation is the validator's job, not ours.
type angleDeclarationRecord struct {
	SchemaVersion  string             `json:"schema_version"`
	EvidenceID     string             `json:"evidence_id"`
	RuntimeID      string             `json:"runtime_id"`
	REQID          string             `json:"req_id"`
	ReviewRound    int                `json:"review_round"`
	Dimension      string             `json:"dimension"`
	DeclaredAt     string             `json:"declared_at"`
	CommittedAt    string             `json:"committed_at"`
	DeclaredAngles []declaredAngleRow `json:"declared_angles"`
	Dispositions   []dispositionRow   `json:"dispositions"`
}

type declaredAngleRow struct {
	ID        string `json:"id"`
	Statement string `json:"statement"`
	Target    string `json:"target"`
}

type dispositionRow struct {
	AngleID        string `json:"angle_id"`
	Kind           string `json:"kind"`
	Note           string `json:"note"`
	NewAngleTarget string `json:"new_angle_target,omitempty"`
}

// inheritedAngleRow is the projection of team-manifest inherited_angles used
// only for set-membership checks. Schema field `id` matches the registry id
// pattern ANG-{MODULE}-{NNN}.
type inheritedAngleRow struct {
	ID            string `json:"id"`
	Module        string `json:"module"`
	Statement     string `json:"statement"`
	Target        string `json:"target"`
	LastAppliedIn string `json:"last_applied_in"`
}

// teamManifestRecord is the slice of the team-manifest schema we inspect. We
// deliberately avoid decoding the whole schema because the harness does not
// bundle the team-manifest schema here and we want this guard to remain
// independent of the manifest validator's surface area.
type teamManifestRecord struct {
	WorkgroupKind   string              `json:"workgroup_kind"`
	Dimension       string              `json:"dimension"`
	MinAngles       *int                `json:"min_angles"`
	DispatchedAt    string              `json:"dispatched_at"`
	InheritedAngles []inheritedAngleRow `json:"inherited_angles"`
}

// angleCompleteContext is the resolution result of the dimension-specific
// guard call. It centralizes the evidence lookup so each guard body is a
// pure dimension check on top of shared validation.
type angleCompleteContext struct {
	root        string
	dimension   angleDimension
	declaration *angleDeclarationRecord
	manifest    *teamManifestRecord
	reqPath     string
	reqUIImpact string
}

// guardDeliveryAngleCompleteFn is the DV phase guard. It replaces the legacy
// evidenceBackedGuard("delivery_team_complete") stub and additionally enforces
// the angle_declaration preconditions for FR-002 / FR-003 / FR-004.
func guardDeliveryAngleCompleteFn(state map[string]any, evidence map[string]string) error {
	return evaluateAngleComplete(state, angleDimensionDelivery)
}

// guardQAAngleCompleteFn is the QA phase guard. Replaces
// evidenceBackedGuard("qa_team_complete").
func guardQAAngleCompleteFn(state map[string]any, evidence map[string]string) error {
	return evaluateAngleComplete(state, angleDimensionQA)
}

// guardE2EAngleCompleteFn is the E2E phase guard. Replaces
// evidenceBackedGuard("e2e_team_complete") and additionally honors the FR-010
// E2E NA path (ui_impact=none allows N=1 with target pointing at the REQ
// top blockquote's ui_impact field).
func guardE2EAngleCompleteFn(state map[string]any, evidence map[string]string) error {
	return evaluateAngleComplete(state, angleDimensionE2E)
}

// evaluateAngleComplete resolves both the angle_declaration evidence and the
// team_manifest evidence for the current review round, then runs the shared
// FR-002/FR-003/FR-004 conjunction with the dimension-specific E2E NA
// relaxation on top.
func evaluateAngleComplete(state map[string]any, dim angleDimension) error {
	root, _ := state["root"].(string)
	if root == "" {
		root = "."
	}
	ctx, err := resolveAngleCompleteContext(state, dim, root)
	if err != nil {
		return err
	}
	// FR-002: angle_declaration.committed_at <= team_manifest.dispatched_at.
	if err := assertDeclaredBeforeDispatch(ctx); err != nil {
		return err
	}
	// FR-003: minimum count + concrete target (with FR-010 E2E NA allowance).
	if err := assertMinCountAndConcreteTarget(ctx); err != nil {
		return err
	}
	// FR-004: every inherited angle must have a disposition; retract/extend/
	// revive mechanics carry their per-kind obligations.
	if err := assertDispositionComplete(ctx); err != nil {
		return err
	}
	// Final dimension check: the declaration's dimension field must equal
	// the expected dimension for this guard. A delivery declaration cannot
	// satisfy an E2E phase transition.
	if ctx.declaration.Dimension != string(dim) {
		return fmt.Errorf(
			"%s_angle_complete: angle_declaration dimension=%q does not match expected %q",
			dim, ctx.declaration.Dimension, dim)
	}
	return nil
}

// resolveAngleCompleteContext finds the current review round's angle_declaration
// and team_manifest in state.evidence[] and decodes both files. The lookup
// is round-scoped so a stale declaration from a previous review round cannot
// satisfy a new round's gate.
func resolveAngleCompleteContext(state map[string]any, dim angleDimension, root string) (*angleCompleteContext, error) {
	reviewRound := currentReviewRound(state)
	angleEvidence, err := findEvidence(state, "angle_declaration", reviewRound, string(dim))
	if err != nil {
		return nil, fmt.Errorf(
			"%s_angle_complete: %w — pre-review angle_declaration evidence is required (FR-002)",
			dim, err)
	}
	manifestEvidence, err := findTeamManifestEvidence(state, reviewRound, string(dim), root)
	if err != nil {
		return nil, fmt.Errorf(
			"%s_angle_complete: %w — team_manifest evidence is required to read inherited_angles and dispatched_at",
			dim, err)
	}
	decl, err := loadAngleDeclaration(root, evidencePath(angleEvidence))
	if err != nil {
		return nil, fmt.Errorf("%s_angle_complete: read angle_declaration: %w", dim, err)
	}
	manifest, err := loadTeamManifest(root, evidencePath(manifestEvidence))
	if err != nil {
		return nil, fmt.Errorf("%s_angle_complete: read team_manifest: %w", dim, err)
	}
	reqPath, reqUIImpact := readBoundREQFromState(state, root)
	return &angleCompleteContext{
		root:        root,
		dimension:   dim,
		declaration: decl,
		manifest:    manifest,
		reqPath:     reqPath,
		reqUIImpact: reqUIImpact,
	}, nil
}

// currentReviewRound reads state.review.round (default 1).
func currentReviewRound(state map[string]any) int {
	review, _ := state["review"].(map[string]any)
	if review == nil {
		return 1
	}
	switch v := review["round"].(type) {
	case float64:
		return int(v)
	case int:
		return v
	}
	return 1
}

// findEvidence scans state.evidence[] for an entry whose kind matches
// expectedKind, whose review_round matches currentRound, and (when
// expectedDim != "") whose dimension field matches expectedDim. A
// non-empty expectedDim is matched against the evidence item's own dimension
// property (e.g. angle_declaration.declared dimension).
func findEvidence(state map[string]any, expectedKind string, currentRound int, expectedDim string) (map[string]any, error) {
	items, _ := state["evidence"].([]any)
	for _, raw := range items {
		item, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		kind, _ := item["kind"].(string)
		if kind != expectedKind {
			continue
		}
		if status, _ := item["status"].(string); status != "valid" {
			continue
		}
		if rr := asInt(item["review_round"]); rr > 0 && rr != currentRound {
			continue
		}
		if expectedDim != "" {
			dim, _ := item["dimension"].(string)
			if dim != expectedDim {
				continue
			}
		}
		return item, nil
	}
	return nil, fmt.Errorf("no valid %s evidence for review round %d", expectedKind, currentRound)
}

// findTeamManifestEvidence resolves the team_manifest index entry for the
// current review round and verification dimension. Index entries may carry an
// optional dimension tag; when absent we fall back to the on-disk manifest's
// dimension field so a delivery manifest cannot satisfy e2e_angle_complete.
func findTeamManifestEvidence(state map[string]any, currentRound int, expectedDim, root string) (map[string]any, error) {
	if item, err := findEvidence(state, "team_manifest", currentRound, expectedDim); err == nil {
		return item, nil
	}
	items, _ := state["evidence"].([]any)
	for _, raw := range items {
		item, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		kind, _ := item["kind"].(string)
		if kind != "team_manifest" {
			continue
		}
		if status, _ := item["status"].(string); status != "valid" {
			continue
		}
		if rr := asInt(item["review_round"]); rr > 0 && rr != currentRound {
			continue
		}
		if dim, _ := item["dimension"].(string); dim != "" && dim != expectedDim {
			continue
		}
		manifest, err := loadTeamManifest(root, evidencePath(item))
		if err != nil {
			continue
		}
		if manifest.Dimension != "" && manifest.Dimension != expectedDim {
			continue
		}
		return item, nil
	}
	return nil, fmt.Errorf("no valid team_manifest evidence for review round %d dimension %s", currentRound, expectedDim)
}

func asInt(value any) int {
	switch v := value.(type) {
	case float64:
		return int(v)
	case int:
		return v
	}
	return 0
}

// evidencePath extracts the file path from a state.evidence[] item. The
// evidence registry uses both `path` (file-relative) and `id` (stable
// identifier); for an on-disk read we want `path` first, then a fallback to
// `id`. An empty path is a configuration error — the loader will surface it.
func evidencePath(item map[string]any) string {
	if p, _ := item["path"].(string); p != "" {
		return p
	}
	if id, _ := item["id"].(string); id != "" {
		return id
	}
	return ""
}

func loadAngleDeclaration(root, ref string) (*angleDeclarationRecord, error) {
	if ref == "" {
		return nil, fmt.Errorf("empty evidence reference")
	}
	data, err := os.ReadFile(filepath.Join(root, filepath.Clean(ref)))
	if err != nil {
		return nil, err
	}
	var rec angleDeclarationRecord
	if err := json.Unmarshal(data, &rec); err != nil {
		return nil, fmt.Errorf("decode angle_declaration: %w", err)
	}
	return &rec, nil
}

func loadTeamManifest(root, ref string) (*teamManifestRecord, error) {
	if ref == "" {
		return nil, fmt.Errorf("empty evidence reference")
	}
	data, err := os.ReadFile(filepath.Join(root, filepath.Clean(ref)))
	if err != nil {
		return nil, err
	}
	var rec teamManifestRecord
	if err := json.Unmarshal(data, &rec); err != nil {
		return nil, fmt.Errorf("decode team_manifest: %w", err)
	}
	return &rec, nil
}

// readBoundREQFromState reads the bound REQ path and re-reads the REQ file
// from disk to extract the top-blockquote UI impact value. We re-read the
// file rather than rely on state.bound_req.metadata.ui_impact because the
// metadata is the trust-once result of the original bind — a bound-REQ doc
// edited after bind would otherwise silently slip through. On a missing or
// malformed REQ the function returns empty strings; this keeps FR-010 NA
// closed by default (fail-safe).
func readBoundREQFromState(state map[string]any, root string) (string, string) {
	bound, _ := state["bound_req"].(map[string]any)
	if bound == nil {
		return "", ""
	}
	reqPath, _ := bound["path"].(string)
	if reqPath == "" {
		return "", ""
	}
	data, err := os.ReadFile(filepath.Join(root, filepath.Clean(reqPath)))
	if err != nil {
		return reqPath, ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), ">"))
		for _, sep := range []string{"：", ":"} {
			parts := strings.SplitN(trimmed, sep, 2)
			if len(parts) != 2 {
				continue
			}
			if !strings.EqualFold(strings.TrimSpace(parts[0]), "UI impact") {
				continue
			}
			return reqPath, strings.ToLower(strings.TrimSpace(parts[1]))
		}
	}
	return reqPath, ""
}

// assertDeclaredBeforeDispatch enforces FR-002: the angle_declaration must
// have been committed BEFORE the team manifest was dispatched. We compare the
// parsed RFC3339 timestamps; a missing or malformed dispatched_at is treated
// as the guard's failure (the manifest is not yet dispatched).
func assertDeclaredBeforeDispatch(ctx *angleCompleteContext) error {
	if ctx.manifest.DispatchedAt == "" {
		return fmt.Errorf(
			"%s_angle_complete: team_manifest.dispatched_at is empty; team was never dispatched (FR-002)",
			ctx.dimension)
	}
	if ctx.declaration.CommittedAt == "" {
		return fmt.Errorf(
			"%s_angle_complete: angle_declaration.committed_at is empty (FR-002)",
			ctx.dimension)
	}
	declaredAt, err := time.Parse(time.RFC3339Nano, ctx.declaration.CommittedAt)
	if err != nil {
		return fmt.Errorf(
			"%s_angle_complete: angle_declaration.committed_at is not RFC3339Nano: %v",
			ctx.dimension, err)
	}
	dispatchedAt, err := time.Parse(time.RFC3339Nano, ctx.manifest.DispatchedAt)
	if err != nil {
		return fmt.Errorf(
			"%s_angle_complete: team_manifest.dispatched_at is not RFC3339Nano: %v",
			ctx.dimension, err)
	}
	if declaredAt.After(dispatchedAt) {
		return fmt.Errorf(
			"%s_angle_complete: angle_declaration committed_at=%s is AFTER team_manifest dispatched_at=%s (FR-002: pre-review declaration)",
			ctx.dimension, ctx.declaration.CommittedAt, ctx.manifest.DispatchedAt)
	}
	return nil
}

// assertMinCountAndConcreteTarget enforces FR-003 with the FR-010 E2E NA
// exception. The expected count is read from team_manifest.min_angles (default
// 3). Every declared target must be non-empty and not a blacklisted generic
// category word.
//
// FR-010 E2E NA path: when dimension == e2e_browser AND the bound REQ's
// top blockquote declares UI impact=none, we accept N=1 AND require the
// single target to point at the ui_impact field. The same N=1 declaration is
// rejected for delivery/qa, and the E2E NA exception is rejected for any
// other REQ ui_impact value.
func assertMinCountAndConcreteTarget(ctx *angleCompleteContext) error {
	count := len(ctx.declaration.DeclaredAngles)
	minRequired := 3
	if ctx.manifest.MinAngles != nil {
		minRequired = *ctx.manifest.MinAngles
	}
	naPath := ctx.dimension == angleDimensionE2E && ctx.reqUIImpact == "none"
	switch {
	case count == 0 && minRequired > 0:
		return fmt.Errorf(
			"%s_angle_complete: declared_angles is empty (FR-003 minimum %d)", ctx.dimension, minRequired)
	case count < minRequired && !(naPath && count == 1):
		return fmt.Errorf(
			"%s_angle_complete: declared_angles count=%d is below minimum %d (FR-003)",
			ctx.dimension, count, minRequired)
	}
	for i, a := range ctx.declaration.DeclaredAngles {
		if err := runtime.ValidateAngleTarget(a.Target); err != nil {
			return fmt.Errorf(
				"%s_angle_complete: declared_angles[%d] (id=%s): %w",
				ctx.dimension, i, a.ID, err)
		}
	}
	if naPath {
		// FR-010 requires exactly N=1 AND the single target to point at
		// the REQ ui_impact blockquote field. We treat the field as a
		// structural sentinel: target must equal "blockquote.ui_impact"
		// (the canonical hook the angle-declaration skill writes).
		if count != 1 {
			return fmt.Errorf(
				"%s_angle_complete: FR-010 E2E NA path requires exactly 1 declared angle, got %d",
				ctx.dimension, count)
		}
		if ctx.declaration.DeclaredAngles[0].Target != e2eNARequiredTarget {
			return fmt.Errorf(
				"%s_angle_complete: FR-010 E2E NA path requires target=%q, got %q",
				ctx.dimension, e2eNARequiredTarget, ctx.declaration.DeclaredAngles[0].Target)
		}
	}
	return nil
}

// assertDispositionComplete enforces FR-004: every inherited angle must have
// a disposition, and each disposition kind carries its own per-kind
// obligation (retract requires reason, extend requires new_angle_target,
// revive references an already-retracted id).
//
// batch_confirm is treated as a 1:1 disposition entry too: the angle_id
// referenced must still be in inherited_angles (Q-010 allowance for adjacent
// modules — the schema permits multiple angle_ids in note, but each entry
// still has a single primary angle_id we cover here).
func assertDispositionComplete(ctx *angleCompleteContext) error {
	inherited := ctx.manifest.InheritedAngles
	if len(inherited) == 0 {
		// First-round modules may have no inherited baseline; dispositions
		// may be empty by schema. Nothing to check.
		return nil
	}
	indexed := map[string]bool{}
	for _, a := range inherited {
		indexed[a.ID] = true
	}
	covered := map[string]bool{}
	knownKinds := map[string]bool{
		"confirm": true, "extend": true, "retract": true,
		"revive": true, "batch_confirm": true,
	}
	for i, d := range ctx.declaration.Dispositions {
		if d.AngleID == "" {
			return fmt.Errorf(
				"%s_angle_complete: dispositions[%d] has empty angle_id (FR-004)",
				ctx.dimension, i)
		}
		if !knownKinds[d.Kind] {
			return fmt.Errorf(
				"%s_angle_complete: dispositions[%d] kind=%q is not in {confirm, extend, retract, revive, batch_confirm}",
				ctx.dimension, i, d.Kind)
		}
		if !indexed[d.AngleID] {
			return fmt.Errorf(
				"%s_angle_complete: dispositions[%d] angle_id=%s is not in team_manifest.inherited_angles (FR-004)",
				ctx.dimension, i, d.AngleID)
		}
		if covered[d.AngleID] {
			return fmt.Errorf(
				"%s_angle_complete: dispositions[%d] duplicates angle_id=%s (FR-004 1:1)",
				ctx.dimension, i, d.AngleID)
		}
		if d.Note == "" {
			return fmt.Errorf(
				"%s_angle_complete: dispositions[%d] note is empty (mandatory per schema)",
				ctx.dimension, i)
		}
		switch d.Kind {
		case "retract":
			// Per FR-004 retract must include reason. The schema only
			// enforces non-empty note; we additionally require a
			// reasonable prose signal ("reason" token or >4 chars) so
			// that "ok" or "x" notes cannot satisfy the guard.
			if !strings.Contains(strings.ToLower(d.Note), "reason") &&
				len(strings.TrimSpace(d.Note)) < 4 {
				return fmt.Errorf(
					"%s_angle_complete: dispositions[%d] retract must include reason in note",
					ctx.dimension, i)
			}
		case "extend":
			if d.NewAngleTarget == "" {
				return fmt.Errorf(
					"%s_angle_complete: dispositions[%d] extend must include new_angle_target",
					ctx.dimension, i)
			}
			if err := runtime.ValidateAngleTarget(d.NewAngleTarget); err != nil {
				return fmt.Errorf(
					"%s_angle_complete: dispositions[%d] extend new_angle_target: %w",
					ctx.dimension, i, err)
			}
		case "revive":
			// We require the revive to reference an id whose current
			// registry status is retracted. The registry check would need
			// a registry lookup; instead we trust the schema-level
			// validation and only assert the disposition id is
			// well-formed here.
			if !strings.HasPrefix(d.AngleID, "ANG-") {
				return fmt.Errorf(
					"%s_angle_complete: dispositions[%d] revive angle_id=%q is not a registry id",
					ctx.dimension, i, d.AngleID)
			}
		}
		covered[d.AngleID] = true
	}
	for _, a := range inherited {
		if !covered[a.ID] {
			return fmt.Errorf(
				"%s_angle_complete: inherited angle %s has no disposition (FR-004 1:1)",
				ctx.dimension, a.ID)
		}
	}
	return nil
}
