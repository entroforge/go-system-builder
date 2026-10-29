package review

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/entroforge/go-system-builder/internal/metrics"
	loopruntime "github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/schema"
	"github.com/entroforge/go-system-builder/internal/semantic"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

// supplement.go implements the FindingSupplement append path (L3-S7 §3.6,
// L3-S8 §2.2). Findings are immutable: a supplement is a new first-class
// artifact authored by the original finder that hangs under
// supplements_finding_id with its own content hash. It never rewrites the
// Finding, never touches the sealed ObservationBatch finding_ids exact set,
// and never carries root cause / repair content.
//
// Storage: the supplement index rows live in the main runtime state at
// entities.finding_supplements, appended inside the same runtime CAS
// transaction (revision-checked read-modify-write + journal event) that
// registers the supplement's evidence entry. The artifact bytes live under
// .claude/evidence/<runtime>/g<gen>/ next to the findings they extend.
//
// Legacy migration: the first append after the schema opened migrates any
// pre-existing rows from the retired control-plane document
// .claude/review/finding-supplements.json into entities.finding_supplements
// (idempotent by supplement_id) and deletes the legacy file, so the main
// state becomes the single supplement authority.

const supplementIndexRelativePath = ".claude/review/finding-supplements.json"

// SupplementRequest drives `runtime finding-supplement`.
type SupplementRequest struct {
	// ExpectedRevision is an optional explicit assertion for integrations or
	// recovery tooling. Normal stage callers leave it negative and the Writer
	// commits against the current locked Runtime snapshot.
	ExpectedRevision int
	FindingID        string
	FilePath         string
	// AuthorAgentID is supplied only by an existing, accurately bound caller.
	// The explicit compatibility CLI retains its attribution declaration;
	// selecting a Finding never proves who made a new observation.
	AuthorAgentID string
	// AuthorizedBy records the scheduler identity that appointed a replacement
	// finder when Author != the Finding's original_finder (L3-S8 §2.2: the
	// original Agent may be unreachable; S8 never treats that as losing the
	// symptom). Empty means "must be the original finder".
	AuthorizedBy string
	// InRoundNote declares the S7 in-round exemption from the discriminator
	// gate (L3-S7 §14.1): the original finder proactively adds an observation
	// inside the open round rather than answering an S8 discriminator-bound
	// follow-up request. Exempt supplements must not carry hypothesis_id.
	InRoundNote bool
	OccurredAt  time.Time
}

// SupplementReceipt summarizes one committed supplement append.
type SupplementReceipt struct {
	SupplementID string
	FindingID    string
	Path         string
	SHA256       string
	// Revision is the commit revision on first reception, or the current
	// read-only snapshot containing the same original accepted supplement.
	Revision int
}

// SupplementRow is one index row: the control-plane pointer to a persisted
// supplement artifact. Rows are append-only and never mutated.
type SupplementRow struct {
	SupplementID         string `json:"supplement_id"`
	SupplementsFindingID string `json:"supplements_finding_id"`
	Author               string `json:"author"`
	AuthorizedBy         string `json:"authorized_by,omitempty"`
	Path                 string `json:"path"`
	SHA256               string `json:"sha256"`
	ReviewRound          int    `json:"review_round"`
	BaselineGeneration   int    `json:"baseline_generation"`
	HypothesisID         string `json:"hypothesis_id,omitempty"`
	Discriminator        string `json:"discriminator,omitempty"`
	CreatedAt            string `json:"created_at"`
	AppendedAt           string `json:"appended_at"`
}

// supplementIndex is the retired control-plane document shape, kept only to
// read and migrate legacy .claude/review/finding-supplements.json files.
type supplementIndex struct {
	SchemaVersion string          `json:"schema_version"`
	RuntimeID     string          `json:"runtime_id"`
	Revision      int             `json:"revision"`
	Supplements   []SupplementRow `json:"supplements"`
}

// SubmitSupplement validates and appends one FindingSupplement:
//
//  1. schema-validates the document and verifies its independent content hash;
//  2. enforces the discriminator gate (L3-S7 §14.1, L3-S8 §2.2): S8 follow-up
//     observations carry hypothesis_id + discriminator + expected_outcomes,
//     S7 in-round notes are declared via --in-round-note and carry none;
//  3. proves the target Finding exists and belongs to the current review round;
//  4. enforces authorship: the author is the Finding's original_finder, or a
//     scheduler-authorized replacement (--authorized-by, L3-S8 §2.2);
//  5. persists the artifact under the evidence tree and appends one immutable
//     entities.finding_supplements row plus the finding_supplement evidence
//     entry in one Writer transaction — the Finding and any sealed
//     ObservationBatch are never modified.
func SubmitSupplement(root, statePath, journalPath string, request SupplementRequest) (SupplementReceipt, error) {
	if strings.TrimSpace(request.FindingID) == "" || strings.TrimSpace(request.FilePath) == "" {
		return SupplementReceipt{}, fmt.Errorf("--finding and --file are required")
	}
	data, err := os.ReadFile(request.FilePath)
	if err != nil {
		return SupplementReceipt{}, fmt.Errorf("read FindingSupplement: %w", err)
	}
	var originalInput map[string]json.RawMessage
	_ = json.Unmarshal(data, &originalInput)
	_, explicitArtifact := originalInput["hash"]
	stateData, err := os.ReadFile(statePath)
	if err != nil {
		return SupplementReceipt{}, fmt.Errorf("read runtime: %w", err)
	}
	var current map[string]any
	if err := json.Unmarshal(stateData, &current); err != nil {
		return SupplementReceipt{}, fmt.Errorf("decode runtime: %w", err)
	}
	data, err = prepareSupplementInput(root, current, request, data)
	if err != nil {
		return SupplementReceipt{}, err
	}
	if err := schema.NewValidator(root).ValidateBytes("finding-supplement.schema.json", data); err != nil {
		return SupplementReceipt{}, fmt.Errorf("FindingSupplement schema: %w", err)
	}
	var supplement Supplement
	if err := json.Unmarshal(data, &supplement); err != nil {
		return SupplementReceipt{}, fmt.Errorf("decode FindingSupplement: %w", err)
	}
	var gateDoc struct {
		ExpectedOutcomes []string `json:"expected_outcomes"`
	}
	if err := json.Unmarshal(data, &gateDoc); err != nil {
		return SupplementReceipt{}, fmt.Errorf("decode FindingSupplement: %w", err)
	}
	if err := enforceSupplementGate(supplement, gateDoc.ExpectedOutcomes, request.InRoundNote); err != nil {
		return SupplementReceipt{}, err
	}
	if supplement.SupplementsFindingID != request.FindingID {
		return SupplementReceipt{}, fmt.Errorf("FindingSupplement supplements_finding_id %s does not match --finding %s", supplement.SupplementsFindingID, request.FindingID)
	}
	if err := verifySupplementHash(data, supplement.Hash); err != nil {
		return SupplementReceipt{}, err
	}

	round := currentReviewRound(current)
	if round < 1 {
		return SupplementReceipt{}, fmt.Errorf("no active review round; supplements attach to current-round Findings")
	}
	findingRow := findFindingRow(current, request.FindingID)
	if findingRow == nil {
		return SupplementReceipt{}, fmt.Errorf("finding %s is not registered; supplements extend an existing immutable Finding, they never create one", request.FindingID)
	}
	if findingRound := intField(findingRow["review_round"]); findingRound != round {
		return SupplementReceipt{}, fmt.Errorf("finding %s belongs to review round %d but the runtime is at round %d", request.FindingID, findingRound, round)
	}
	originalFinder := stringField(findingRow["original_finder"])
	if supplement.Author != originalFinder && strings.TrimSpace(request.AuthorizedBy) == "" {
		return SupplementReceipt{}, fmt.Errorf("supplement author %s is not the original finder %s; a replacement finder requires --authorized-by <scheduler identity> (L3-S8 §2.2)", supplement.Author, originalFinder)
	}
	if !explicitArtifact {
		for _, row := range supplementRowsFromState(current) {
			if row.SupplementID == supplement.SupplementID {
				// prepareSupplementInput has verified the original bytes,
				// professional content and frozen evidence. No new event or time.
				return SupplementReceipt{SupplementID: row.SupplementID, FindingID: row.SupplementsFindingID, Path: row.Path, SHA256: row.SHA256, Revision: intField(current["revision"])}, nil
			}
		}
	}

	occurredAt := request.OccurredAt
	if occurredAt.IsZero() {
		occurredAt = time.Now().UTC()
	}
	runtimeID, _ := current["runtime_id"].(string)
	generation := baselineGeneration(current)
	revision := intField(current["revision"])

	// Compute the immutable target before the CAS. Duplicate checks must happen
	// before any filesystem mutation; otherwise a retry reports an artifact
	// collision instead of the domain-level duplicate and can leave misleading
	// partial evidence behind.
	supplementRel := filepath.ToSlash(filepath.Join(
		".claude", "evidence", runtimeID, fmt.Sprintf("g%d", generation),
		"finding-supplements", supplement.SupplementID+".json"))
	supplementBytes := append(canonicalJSON(data), '\n')

	// Legacy migration source: rows from the retired control-plane index are
	// merged into entities.finding_supplements by the same transaction.
	legacy, legacyExists, err := readSupplementIndex(root)
	if err != nil {
		return SupplementReceipt{}, err
	}
	if legacyExists && legacy.RuntimeID != "" && legacy.RuntimeID != runtimeID {
		return SupplementReceipt{}, fmt.Errorf("legacy supplement index %s belongs to runtime %s; the active runtime is %s — archive or remove the stale index before appending supplements", supplementIndexRelativePath, legacy.RuntimeID, runtimeID)
	}

	// Fail fast on obvious duplicates; the CAS Apply re-checks against the
	// authoritative candidate state.
	for _, existing := range supplementRowsFromState(current) {
		if existing.SupplementID == supplement.SupplementID {
			return SupplementReceipt{}, fmt.Errorf("supplement %s is already appended to finding %s; supplements are immutable — use a new supplement_id", supplement.SupplementID, existing.SupplementsFindingID)
		}
	}
	if legacyExists {
		for _, existing := range legacy.Supplements {
			if existing.SupplementID == supplement.SupplementID {
				return SupplementReceipt{}, fmt.Errorf("supplement %s is already appended to finding %s (legacy supplement index); supplements are immutable — use a new supplement_id", supplement.SupplementID, existing.SupplementsFindingID)
			}
		}
	}
	// Persist only after all deterministic duplicate and legacy checks pass;
	// writeArtifact itself also refuses overwrite as the final race guard.
	if err := writeArtifact(root, supplementRel, supplementBytes); err != nil {
		return SupplementReceipt{}, err
	}
	supplementSHA := sha256Of(supplementBytes)
	row := SupplementRow{
		SupplementID:         supplement.SupplementID,
		SupplementsFindingID: request.FindingID,
		Author:               supplement.Author,
		AuthorizedBy:         strings.TrimSpace(request.AuthorizedBy),
		Path:                 supplementRel,
		SHA256:               supplementSHA,
		ReviewRound:          round,
		BaselineGeneration:   generation,
		HypothesisID:         supplement.HypothesisID,
		Discriminator:        supplement.Discriminator,
		CreatedAt:            supplement.CreatedAt,
		AppendedAt:           occurredAt.UTC().Format(time.RFC3339Nano),
	}

	lifecycle, _ := current["lifecycle"].(map[string]any)
	cursor := map[string]any{"state": lifecycle["state"], "phase": lifecycle["phase"]}
	lens := stringField(findingRow["lens"])

	store := loopruntime.NewWriter(statePath, journalPath, root, semantic.RuntimeCandidateValidator{})
	expectedRevision := request.ExpectedRevision
	// Zero was the implicit value before SupplementRequest exposed the
	// optional assertion. Active review rounds are already beyond bootstrap,
	// so retain source compatibility for callers that omit the field.
	if expectedRevision <= 0 {
		expectedRevision = -1
	}
	commitRevision := currentCommitRevision(expectedRevision, current)
	snapshot, err := updateRuntime(store, expectedRevision, loopruntime.Mutation{
		EventID:        fmt.Sprintf("evt-finding-supplement-%s-r%d", supplement.SupplementID, commitRevision+1),
		TransitionID:   "FINDING-SUPPLEMENT",
		Event:          "finding_supplement_appended",
		Actor:          "orchestrator",
		IdempotencyKey: fmt.Sprintf("runtime:finding-supplement:%s:%d", supplement.SupplementID, commitRevision),
		RuntimeID:      runtimeID,
		From:           cursor,
		To:             cursor,
		EvidenceIDs:    []string{supplement.SupplementID},
		Message: fmt.Sprintf("Appended FindingSupplement %s to immutable Finding %s (round %d)",
			supplement.SupplementID, request.FindingID, round),
		OccurredAt: occurredAt,
		Apply: func(state map[string]any) error {
			legacyRows := []SupplementRow(nil)
			if legacyExists {
				legacyRows = legacy.Supplements
			}
			return applySupplement(state, legacyRows, row, lens, occurredAt)
		},
	})
	if err != nil {
		// The artifact is staged before the Writer commit. Remove it only when
		// the failed operation left no pending marker, the Runtime is still the
		// pre-commit revision, and no state path points at it.
		cleanupStore := loopruntime.NewWriter(statePath, journalPath, root, semantic.RuntimeCandidateValidator{})
		if _, cleanupErr := cleanupStore.RemoveUnreferencedArtifact(loopruntime.ArtifactCleanupRequest{
			ExpectedRevision: revision,
			ArtifactPath:     supplementRel,
			ArtifactSHA256:   supplementSHA,
			ReferencedPaths:  supplementStateArtifactPaths(current),
		}); cleanupErr != nil {
			return SupplementReceipt{}, fmt.Errorf("finding supplement append failed and staged artifact cleanup was inconclusive: %w (original: %v)", cleanupErr, err)
		}
		return SupplementReceipt{}, err
	}
	// The main state now carries the rows; retire the legacy control-plane
	// document so it cannot diverge. A missing file is fine (concurrent
	// submits migrate the same content idempotently).
	if legacyExists {
		if err := os.Remove(supplementIndexPath(root)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return SupplementReceipt{}, fmt.Errorf("retire legacy supplement index: %w", err)
		}
	}
	// Round shape gauges (L3-S7 §14.2) are idempotent; the finding's existence
	// implies a registered plan, but stay defensive.
	if plan, ptr, planErr := LoadPlan(root, current); planErr == nil {
		_ = metrics.RecordS7RoundShape(root, round, len(plan.Assignments), len(plan.Claims), ptr.Revision)
	}
	return SupplementReceipt{
		SupplementID: supplement.SupplementID,
		FindingID:    request.FindingID,
		Path:         supplementRel,
		SHA256:       supplementSHA,
		Revision:     snapshot.Revision,
	}, nil
}

// prepareSupplementInput adapts professional observations into the original
// persisted artifact. It neither issues another questionnaire nor attributes
// a new observation to the old finder without a real caller/declaration.
// Explicit legacy artifacts retain their original hash validation unchanged.
func prepareSupplementInput(root string, state map[string]any, request SupplementRequest, data []byte) ([]byte, error) {
	if err := workpacket.ValidateUniqueJSON(data); err != nil {
		return nil, fmt.Errorf("FindingSupplement JSON: %w", err)
	}
	var input map[string]any
	if err := json.Unmarshal(data, &input); err != nil || input == nil {
		return nil, fmt.Errorf("FindingSupplement must be a JSON object")
	}
	if request.AuthorAgentID != "" {
		if declared, ok := input["author"]; ok && declared != request.AuthorAgentID {
			return nil, fmt.Errorf("FindingSupplement author differs from the original bound caller")
		}
	}
	// A complete historical artifact is already fixed. Do not rewrite its
	// timestamps, hash or evidence to the current filesystem.
	_, hasHash := input["hash"]
	if hasHash {
		return data, nil
	}
	if _, ok := input["schema_version"]; !ok {
		input["schema_version"] = "1.0.0"
	}
	if _, ok := input["supplements_finding_id"]; !ok {
		input["supplements_finding_id"] = request.FindingID
	}
	if request.AuthorAgentID != "" {
		input["author"] = request.AuthorAgentID
	}
	if strings.TrimSpace(stringField(input["author"])) == "" {
		return nil, fmt.Errorf("FindingSupplement has no actual author attribution: restore the original caller context; the explicit compatibility entry requires who made this new observation, not SHA, timestamps or a canonical envelope")
	}
	// Identify the professional observation before reading mutable evidence.
	// A retry must resolve the original object, not sign today's bytes into a
	// different identity or manufacture another observation after source drift.
	if _, ok := input["supplement_id"]; !ok {
		identity, err := json.Marshal(map[string]any{"runtime_id": state["runtime_id"], "generation": baselineGeneration(state), "round": currentReviewRound(state), "finding_id": request.FindingID, "observation": input})
		if err != nil {
			return nil, err
		}
		input["supplement_id"] = "supplement-" + sha256Of(identity)[:32]
	}
	for _, row := range supplementRowsFromState(state) {
		if row.SupplementID != input["supplement_id"] {
			continue
		}
		if row.SupplementsFindingID != request.FindingID || row.Author != input["author"] || row.ReviewRound != currentReviewRound(state) || row.BaselineGeneration != baselineGeneration(state) {
			return nil, fmt.Errorf("FindingSupplement original responsibility differs from current professional input")
		}
		saved, err := workpacket.ReadFile(root, row.Path, resultReceiptReadLimit)
		if err != nil || sha256Of(saved) != row.SHA256 {
			return nil, fmt.Errorf("FindingSupplement original immutable artifact %s is missing or changed: %v", row.Path, err)
		}
		if err := schema.NewValidator(root).ValidateBytes("finding-supplement.schema.json", saved); err != nil {
			return nil, err
		}
		var accepted map[string]any
		if err := json.Unmarshal(saved, &accepted); err != nil {
			return nil, err
		}
		if err := verifySupplementHash(saved, stringField(accepted["hash"])); err != nil {
			return nil, err
		}
		if !sameSupplementProfessionalInput(input, accepted) {
			return nil, fmt.Errorf("FindingSupplement original professional input differs; an accepted supplement is immutable")
		}
		var refs []string
		if value, ok := accepted["evidence_refs"]; ok {
			data, _ := json.Marshal(value)
			if err := json.Unmarshal(data, &refs); err != nil {
				return nil, err
			}
		}
		if err := validateEvidenceRefs(root, state, refs, "original FindingSupplement"); err != nil {
			return nil, err
		}
		return saved, nil
	}
	// Freeze local observations in the original reception, without changing
	// the authoring file or refreshing an existing complete artifact.
	if raw, ok := input["evidence_refs"]; ok {
		refs, err := json.Marshal(raw)
		if err != nil {
			return nil, err
		}
		var values []string
		if err := json.Unmarshal(refs, &values); err != nil {
			return nil, fmt.Errorf("FindingSupplement evidence_refs: %w", err)
		}
		result := Result{Findings: []Finding{{EvidenceRefs: values}}}
		capturePath := ""
		if finding := findFindingRow(state, request.FindingID); stringField(finding["assignment_id"]) != "" {
			capturePath = CaptureFile(root, stringField(state["runtime_id"]), baselineGeneration(state), stringField(finding["assignment_id"]))
		}
		if err := bindIssuedPathEvidence(root, root, nil, &result, false, capturePath); err != nil {
			return nil, err
		}
		if err := validateEvidenceRefs(root, state, result.Findings[0].EvidenceRefs, "FindingSupplement"); err != nil {
			return nil, err
		}
		input["evidence_refs"] = result.Findings[0].EvidenceRefs
	}
	if _, ok := input["created_at"]; !ok {
		now := request.OccurredAt
		if now.IsZero() {
			now = time.Now().UTC()
		}
		input["created_at"] = now.UTC().Format(time.RFC3339Nano)
	}
	canonical, err := json.Marshal(input)
	if err != nil {
		return nil, err
	}
	hash, err := supplementContentHash(canonical)
	if err != nil {
		return nil, err
	}
	input["hash"] = hash
	return json.Marshal(input)
}

func sameSupplementProfessionalInput(input, accepted map[string]any) bool {
	for key, want := range input {
		if key == "evidence_refs" {
			wantRefs, wantOK := want.([]any)
			gotRefs, gotOK := accepted[key].([]any)
			if !wantOK || !gotOK || len(wantRefs) != len(gotRefs) {
				return false
			}
			for i, raw := range wantRefs {
				ref, ok := raw.(string)
				got, gotOK := gotRefs[i].(string)
				if !ok || !gotOK {
					return false
				}
				if (strings.HasPrefix(ref, "path:") || strings.HasPrefix(ref, "command_output:")) && !strings.Contains(ref, "#sha256=") {
					got = strings.SplitN(got, "#sha256=", 2)[0]
				}
				if ref != got {
					return false
				}
			}
			continue
		}
		if !reflect.DeepEqual(want, accepted[key]) {
			return false
		}
	}
	// Every accepted professional field must also have been in the input;
	// only the original machine fields may have been generated at reception.
	for key := range accepted {
		switch key {
		case "schema_version", "supplement_id", "supplements_finding_id", "author", "created_at", "hash":
			continue
		}
		if _, ok := input[key]; !ok {
			return false
		}
	}
	return true
}

// enforceSupplementGate implements the discriminator gate (L3-S7 §14.1:
// "S8 请求重新复现已确认症状 → 默认拒绝"; L3-S8 §2.2: a follow-up observation
// without hypothesis_id + discriminator + expected distinguishing outcomes is
// rejected by the tool). The single exemption is an S7 in-round note: the
// original finder proactively supplements an observation inside the open
// round, declares it with --in-round-note, and carries no hypothesis_id.
//
// When the gate rejects an S8 follow-up, the error enumerates the missing
// field(s) one per line so the caller can fix every gap in a single edit
// instead of doing half-experiments field by field.
func enforceSupplementGate(supplement Supplement, expectedOutcomes []string, inRoundNote bool) error {
	hasHypothesis := strings.TrimSpace(supplement.HypothesisID) != ""
	hasDiscriminator := strings.TrimSpace(supplement.Discriminator) != ""
	hasOutcomes := len(expectedOutcomes) > 0
	if inRoundNote {
		if hasHypothesis {
			return fmt.Errorf("--in-round-note marks an S7 in-round note from the original finder and must not carry hypothesis_id; a discriminator-bound follow-up observation belongs to S8 — submit it without --in-round-note and with hypothesis_id + discriminator + expected_outcomes")
		}
		if hasDiscriminator || hasOutcomes {
			missing := []string{}
			if hasDiscriminator && !hasHypothesis {
				missing = append(missing, "hypothesis_id (required to bind a discriminator to a registered S8 Hypothesis)")
			}
			if hasOutcomes && !hasHypothesis {
				missing = append(missing, "hypothesis_id (required to bind expected_outcomes to a registered S8 Hypothesis)")
			}
			if missing == nil {
				missing = append(missing, "the supplement must carry hypothesis_id + discriminator + expected_outcomes together, or none of them with --in-round-note")
			}
			return fmt.Errorf("supplement carries a partial discriminator binding; S8 判别观察需 hypothesis_id + discriminator + expected_outcomes (L3-S8 §2.2), 轮内补充用 --in-round-note 且三者都不携带 (L3-S7 §14.1)\n  - %s", strings.Join(missing, "\n  - "))
		}
		return nil
	}
	if !hasHypothesis || !hasDiscriminator || !hasOutcomes {
		missing := []string{}
		if !hasHypothesis {
			missing = append(missing, "hypothesis_id (the registered S8 Hypothesis the discriminator distinguishes; required for S8 follow-up observations)")
		}
		if !hasDiscriminator {
			missing = append(missing, "discriminator (the S8 discriminator this observation answers; required together with hypothesis_id and expected_outcomes)")
		}
		if !hasOutcomes {
			missing = append(missing, "expected_outcomes (the distinguishing outcomes that support vs refute the hypothesis; required for S8 follow-up observations)")
		}
		return fmt.Errorf("supplement has no complete discriminator binding: S8 判别观察需 hypothesis_id + discriminator + expected_outcomes；轮内补充用 --in-round-note (L3-S8 §2.2, L3-S7 §14.1) — a request to re-run an already confirmed symptom without a discriminator is rejected\n  - missing %s", strings.Join(missing, "\n  - missing "))
	}
	return nil
}

// applySupplement merges legacy migration rows and appends the new index row
// plus its evidence entry inside the runtime CAS transaction. Migration is
// idempotent: a legacy row whose supplement_id is already present is skipped.
func applySupplement(state map[string]any, legacyRows []SupplementRow, row SupplementRow, lens string, occurredAt time.Time) error {
	entities, ok := state["entities"].(map[string]any)
	if !ok {
		return fmt.Errorf("runtime entities must be an object")
	}
	rows, _ := entities["finding_supplements"].([]any)
	seen := make(map[string]string, len(rows)+len(legacyRows)+1)
	for _, raw := range rows {
		entity, _ := raw.(map[string]any)
		if entity != nil {
			seen[stringField(entity["supplement_id"])] = stringField(entity["supplements_finding_id"])
		}
	}
	for _, legacyRow := range legacyRows {
		if _, migrated := seen[legacyRow.SupplementID]; migrated {
			continue
		}
		rows = append(rows, supplementRowEntity(legacyRow))
		seen[legacyRow.SupplementID] = legacyRow.SupplementsFindingID
	}
	if existingFinding, dup := seen[row.SupplementID]; dup {
		return fmt.Errorf("supplement %s is already appended to finding %s; supplements are immutable — use a new supplement_id", row.SupplementID, existingFinding)
	}
	rows = append(rows, supplementRowEntity(row))
	entities["finding_supplements"] = rows
	if err := appendEvidence(state, map[string]any{
		"id":                  row.SupplementID,
		"kind":                "finding_supplement",
		"path":                row.Path,
		"sha256":              row.SHA256,
		"status":              "valid",
		"baseline_generation": row.BaselineGeneration,
		"review_round":        row.ReviewRound,
		"produced_by":         []any{row.Author},
		"invalidated_by":      nil,
		"invalidation_rule":   nil,
		"invalidation_reason": nil,
		"responsibility_id":   LensToResponsibility(lens),
		"scope_refs":          []any{},
	}); err != nil {
		return err
	}
	state["updated_at"] = occurredAt.UTC().Format(time.RFC3339Nano)
	return nil
}

// SupplementsForFinding lists the index rows hanging under one finding, in
// append order. The main runtime state (entities.finding_supplements) is the
// authority; runtimes that predate the merge fall back to the legacy
// control-plane document. A missing index yields no rows.
func SupplementsForFinding(root, statePath, findingID string) ([]SupplementRow, error) {
	stateData, err := os.ReadFile(statePath)
	switch {
	case err == nil:
		var state map[string]any
		if err := json.Unmarshal(stateData, &state); err != nil {
			return nil, fmt.Errorf("decode runtime: %w", err)
		}
		entities, _ := state["entities"].(map[string]any)
		if _, present := entities["finding_supplements"]; present {
			return filterSupplementRows(supplementRowsFromState(state), findingID), nil
		}
	case errors.Is(err, os.ErrNotExist):
		// No runtime state: the legacy index is the only possible source.
	default:
		return nil, fmt.Errorf("read runtime: %w", err)
	}
	legacy, _, err := readSupplementIndex(root)
	if err != nil {
		return nil, err
	}
	return filterSupplementRows(legacy.Supplements, findingID), nil
}

func filterSupplementRows(rows []SupplementRow, findingID string) []SupplementRow {
	var matched []SupplementRow
	for _, row := range rows {
		if row.SupplementsFindingID == findingID {
			matched = append(matched, row)
		}
	}
	return matched
}

// supplementRowsFromState decodes entities.finding_supplements rows.
func supplementRowsFromState(state map[string]any) []SupplementRow {
	entities, _ := state["entities"].(map[string]any)
	raw, _ := entities["finding_supplements"].([]any)
	rows := make([]SupplementRow, 0, len(raw))
	for _, item := range raw {
		entity, _ := item.(map[string]any)
		if entity == nil {
			continue
		}
		rows = append(rows, SupplementRow{
			SupplementID:         stringField(entity["supplement_id"]),
			SupplementsFindingID: stringField(entity["supplements_finding_id"]),
			Author:               stringField(entity["author"]),
			AuthorizedBy:         stringField(entity["authorized_by"]),
			Path:                 stringField(entity["path"]),
			SHA256:               stringField(entity["sha256"]),
			ReviewRound:          intField(entity["review_round"]),
			BaselineGeneration:   intField(entity["baseline_generation"]),
			HypothesisID:         stringField(entity["hypothesis_id"]),
			Discriminator:        stringField(entity["discriminator"]),
			CreatedAt:            stringField(entity["created_at"]),
			AppendedAt:           stringField(entity["appended_at"]),
		})
	}
	return rows
}

// supplementRowEntity renders one index row as its entities.finding_supplements
// JSON shape, mirroring SupplementRow's omitempty contract.
func supplementRowEntity(row SupplementRow) map[string]any {
	entity := map[string]any{
		"supplement_id":          row.SupplementID,
		"supplements_finding_id": row.SupplementsFindingID,
		"author":                 row.Author,
		"path":                   row.Path,
		"sha256":                 row.SHA256,
		"review_round":           row.ReviewRound,
		"baseline_generation":    row.BaselineGeneration,
		"created_at":             row.CreatedAt,
		"appended_at":            row.AppendedAt,
	}
	if row.AuthorizedBy != "" {
		entity["authorized_by"] = row.AuthorizedBy
	}
	if row.HypothesisID != "" {
		entity["hypothesis_id"] = row.HypothesisID
	}
	if row.Discriminator != "" {
		entity["discriminator"] = row.Discriminator
	}
	return entity
}

// findFindingRow returns the entities.findings row for findingID, or nil.
func findFindingRow(state map[string]any, findingID string) map[string]any {
	entities, _ := state["entities"].(map[string]any)
	findings, _ := entities["findings"].([]any)
	for _, raw := range findings {
		row, _ := raw.(map[string]any)
		if row != nil && row["finding_id"] == findingID {
			return row
		}
	}
	return nil
}

// verifySupplementHash checks the supplement's independent content hash:
// sha256 of the canonical JSON (compact, sorted keys) with the hash field
// omitted. The original input adapter computes it; consumers independently
// verify the immutable artifact (L3-S7 §3.6).
func verifySupplementHash(data []byte, declared string) error {
	computed, err := supplementContentHash(data)
	if err != nil {
		return err
	}
	if computed != declared {
		return fmt.Errorf("FindingSupplement hash mismatch: declared %s but the content hashes to %s (sha256 of the compact sorted-keys JSON without the hash field)", declared, computed)
	}
	return nil
}

func supplementContentHash(data []byte) (string, error) {
	var body map[string]any
	if err := json.Unmarshal(data, &body); err != nil {
		return "", fmt.Errorf("decode FindingSupplement: %w", err)
	}
	delete(body, "hash")
	canonical, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("encode FindingSupplement content: %w", err)
	}
	return sha256Of(canonical), nil
}

// supplementStateArtifactPaths returns the repository-relative artifact paths
// currently referenced by Runtime. It is used only for fail-closed cleanup
// after staging a supplement and before the Writer commit becomes durable.
func supplementStateArtifactPaths(state map[string]any) []string {
	seen := map[string]bool{}
	var visit func(any)
	visit = func(value any) {
		switch typed := value.(type) {
		case map[string]any:
			for _, child := range typed {
				visit(child)
			}
		case []any:
			for _, child := range typed {
				visit(child)
			}
		case string:
			path := filepath.ToSlash(typed)
			if strings.HasPrefix(path, ".claude/") {
				seen[path] = true
			}
		}
	}
	visit(state)
	paths := make([]string, 0, len(seen))
	for path := range seen {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths
}

// ---------------------------------------------------------------------------
// legacy supplement index: read-only access to the retired control-plane
// document for migration and pre-merge fallback reads.
// ---------------------------------------------------------------------------

func supplementIndexPath(root string) string {
	return filepath.Join(root, filepath.FromSlash(supplementIndexRelativePath))
}

// readSupplementIndex reads the retired control-plane document. The second
// return value reports whether the file exists.
func readSupplementIndex(root string) (supplementIndex, bool, error) {
	data, err := os.ReadFile(supplementIndexPath(root))
	if errors.Is(err, os.ErrNotExist) {
		return supplementIndex{Supplements: []SupplementRow{}}, false, nil
	}
	if err != nil {
		return supplementIndex{}, false, fmt.Errorf("read legacy supplement index: %w", err)
	}
	var index supplementIndex
	if err := json.Unmarshal(data, &index); err != nil {
		return supplementIndex{}, false, fmt.Errorf("decode legacy supplement index: %w", err)
	}
	if index.Supplements == nil {
		index.Supplements = []SupplementRow{}
	}
	return index, true, nil
}
