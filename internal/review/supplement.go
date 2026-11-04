package review

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/entroforge/go-system-builder/internal/reviewmeta"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"time"

	loopruntime "github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/schema"
	"github.com/entroforge/go-system-builder/internal/semantic"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

// supplement.go implements the FindingSupplement append path (L3-S7 §3.6,
// L3-S8 §2.2). Findings are immutable: a supplement is a new first-class
// artifact authored by the original finder that hangs under
// supplements_finding_id. It never rewrites the
// Finding,
// and never carries root cause / repair content.
//
// Storage: the supplement index rows live in the main runtime state at
// entities.finding_supplements, appended inside the same runtime CAS
// transaction (revision-checked read-modify-write + journal event) that
// registers the supplement's evidence entry. The artifact bytes live under
// .claude/evidence/<runtime>/g<gen>/ next to the findings they extend.
//
// SupplementRequest describes an original finding observation.
type SupplementRequest struct {
	ExpectedRevision int
	FindingID        string
	FilePath         string
	AuthorAgentID    string
	AuthorizedBy     string
	InRoundNote      bool
	OccurredAt       time.Time
}

type SupplementReceipt struct {
	SupplementID string
	FindingID    string
	Path         string
	Revision     int
}

type SupplementRow struct {
	SupplementID         string `json:"supplement_id"`
	SupplementsFindingID string `json:"supplements_finding_id"`
	Author               string `json:"author"`
	AuthorizedBy         string `json:"authorized_by,omitempty"`
	Path                 string `json:"path"`
	ReviewPlanID         string `json:"review_plan_id,omitempty"`
	BaselineGeneration   int    `json:"baseline_generation"`
	HypothesisID         string `json:"hypothesis_id,omitempty"`
	Discriminator        string `json:"discriminator,omitempty"`
	CreatedAt            string `json:"created_at"`
	AppendedAt           string `json:"appended_at"`
}

// SubmitSupplement validates and appends one FindingSupplement:
//
//  1. schema-validates the original professional document;
//  2. enforces the discriminator gate (L3-S7 §14.1, L3-S8 §2.2): S8 follow-up
//     observations carry hypothesis_id + discriminator + expected_outcomes,
//     S7 in-round notes are declared via --in-round-note and carry none;
//  3. proves the target Finding exists and belongs to the current review round;
//  4. enforces authorship: the author is the Finding's original_finder, or a
//     scheduler-authorized replacement (--authorized-by, L3-S8 §2.2);
//  5. persists the artifact under the evidence tree and appends one immutable
//     entities.finding_supplements row plus the finding_supplement evidence
//     entry in one Writer transaction — the original Finding is never modified.
func SubmitSupplement(root, statePath, journalPath string, request SupplementRequest) (SupplementReceipt, error) {
	for {
		receipt, err := submitSupplementOnce(root, statePath, journalPath, request)
		if !errors.Is(err, loopruntime.ErrStaleRevision) {
			return receipt, err
		}
		// The Runtime write count never qualifies this professional append
		// (L4-revision-usage.md §4/§8, G18 §3.1). Another Writer committed
		// after our observation; re-read its actual original responsibility and
		// retry internally. The append's own protection is the three-piece
		// proof: the received row (the appended supplement record), the
		// journal-level dedup of evt-finding-supplement-<id>, and the
		// object's own version (the Finding's review identity and baseline
		// generation). The staged file is left untouched because ownership was
		// not committed.
		//
		// The retry drops the caller's stale counter so it re-reads the actual
		// original responsibility instead of repeating a binding that already
		// moved (L4-revision-usage.md §4).
		request.ExpectedRevision = -1
	}
}

func submitSupplementOnce(root, statePath, journalPath string, request SupplementRequest) (SupplementReceipt, error) {
	if strings.TrimSpace(request.FindingID) == "" || strings.TrimSpace(request.FilePath) == "" {
		return SupplementReceipt{}, fmt.Errorf("--finding and --file are required")
	}
	data, err := os.ReadFile(request.FilePath)
	if err != nil {
		return SupplementReceipt{}, fmt.Errorf("read FindingSupplement: %w", err)
	}
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
	findingRow := findFindingRow(current, request.FindingID)
	if findingRow == nil {
		return SupplementReceipt{}, fmt.Errorf("finding %s is not registered; supplements extend an existing immutable Finding, they never create one", request.FindingID)
	}
	if reviewmeta.CompareIdentity(rowReviewIdentity(findingRow), activeReviewIdentity(current)) == reviewmeta.Different {
		return SupplementReceipt{}, fmt.Errorf("finding %s belongs to a different review plan", request.FindingID)
	}
	originalFinder := stringField(findingRow["original_finder"])
	if supplement.Author != originalFinder && strings.TrimSpace(request.AuthorizedBy) == "" {
		return SupplementReceipt{}, fmt.Errorf("supplement author %s is not the original finder %s; a replacement finder requires --authorized-by <scheduler identity> (L3-S8 §2.2)", supplement.Author, originalFinder)
	}
	for _, row := range supplementRowsFromState(current) {
		if row.SupplementID == supplement.SupplementID {
			// prepareSupplementInput identified the same accepted professional
			// expression for this original responsibility; no new event is needed.
			return SupplementReceipt{SupplementID: row.SupplementID, FindingID: row.SupplementsFindingID, Path: row.Path, Revision: intField(current["revision"])}, nil
		}
	}

	occurredAt := request.OccurredAt
	if occurredAt.IsZero() {
		occurredAt = time.Now().UTC()
	}
	runtimeID, _ := current["runtime_id"].(string)
	generation := baselineGeneration(current)
	// Compute the immutable target before the CAS. Duplicate checks must happen
	// before any filesystem mutation; otherwise a retry reports an artifact
	// collision instead of the domain-level duplicate and can leave misleading
	// partial evidence behind.
	supplementRel := filepath.ToSlash(filepath.Join(
		".claude", "evidence", runtimeID, fmt.Sprintf("g%d", generation),
		"finding-supplements", supplement.SupplementID+".json"))
	supplementBytes := append(canonicalJSON(data), '\n')

	// Fail fast on obvious duplicates; the CAS Apply re-checks against the
	// authoritative candidate state.
	for _, existing := range supplementRowsFromState(current) {
		if existing.SupplementID == supplement.SupplementID {
			return SupplementReceipt{}, fmt.Errorf("supplement %s is already appended to finding %s; supplements are immutable — use a new supplement_id", supplement.SupplementID, existing.SupplementsFindingID)
		}
	}
	// Persist only after the current responsibility duplicate checks pass;
	// writeArtifact itself also refuses overwrite as the final race guard.
	supplementRel, err = publishSupplementArtifact(root, supplementRel, supplementBytes)
	if err != nil {
		return SupplementReceipt{}, err
	}
	row := SupplementRow{
		SupplementID:         supplement.SupplementID,
		SupplementsFindingID: request.FindingID,
		Author:               supplement.Author,
		AuthorizedBy:         strings.TrimSpace(request.AuthorizedBy),
		Path:                 supplementRel,
		ReviewPlanID:         activeReviewIdentity(current).PlanID,
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
	// A negative expected revision is the omission marker: the caller did not
	// assert a Runtime revision. A non-negative value is a concrete binding
	// and keeps the store's own CAS semantics (L4-revision-usage.md §4, §10.2),
	// so Go's zero value means "the Runtime is at revision 0" rather than "no
	// assertion". Callers that mean "omitted" pass a negative value.
	if expectedRevision < 0 {
		// The internal CAS ties the generated identity and staged path to the
		// state used to prepare them; it is not an author-supplied condition.
		expectedRevision = intField(current["revision"])
	}
	snapshot, err := updateRuntime(store, expectedRevision, loopruntime.Mutation{
		EventID:        findingSupplementEventID(supplement.SupplementID),
		TransitionID:   "FINDING-SUPPLEMENT",
		Event:          "finding_supplement_appended",
		Actor:          "orchestrator",
		IdempotencyKey: findingSupplementIdempotencyKey(supplement.SupplementID),
		RuntimeID:      runtimeID,
		From:           cursor,
		To:             cursor,
		EvidenceIDs:    []string{supplement.SupplementID},
		Message:        fmt.Sprintf("Appended FindingSupplement %s to immutable Finding %s", supplement.SupplementID, request.FindingID),
		OccurredAt:     occurredAt,
		Apply: func(state map[string]any) error {
			return applySupplement(state, row, lens, occurredAt)
		},
	})
	if err != nil {
		// A staged file without a committed reference has unknown ownership;
		// leave it intact rather than granting deletion using a digest.
		return SupplementReceipt{}, err
	}
	return SupplementReceipt{
		SupplementID: supplement.SupplementID,
		FindingID:    request.FindingID,
		Path:         supplementRel,
		Revision:     snapshot.Revision,
	}, nil
}

// prepareSupplementInput adapts professional observations into the original
// persisted artifact. It neither issues another questionnaire nor attributes
// a new observation to the old finder without a real caller/declaration.
// Historical machine metadata is ignored in memory; the author's file is
// never rewritten.
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
	// Compare only this author's most recent accepted observation for the same
	// Finding, unless the author explicitly names an existing supplement. The
	// saved expression is used to recognize a replay, not to authenticate it.
	explicitID, _ := input["supplement_id"].(string)
	rows := supplementRowsFromState(state)
	for i := len(rows) - 1; i >= 0; i-- {
		row := rows[i]
		if explicitID != "" && row.SupplementID != explicitID {
			continue
		}
		if explicitID == "" && (row.SupplementsFindingID != request.FindingID || row.Author != input["author"]) {
			continue
		}
		if row.SupplementsFindingID != request.FindingID || row.Author != input["author"] || reviewmeta.CompareIdentity(reviewmeta.ArtifactIdentity(row.ReviewPlanID), activeReviewIdentity(state)) == reviewmeta.Different || row.BaselineGeneration != baselineGeneration(state) {
			if explicitID != "" {
				return nil, fmt.Errorf("FindingSupplement original responsibility differs from current professional input")
			}
			continue
		}
		saved, err := workpacket.ReadFile(root, row.Path, resultReceiptReadLimit)
		if err != nil {
			if explicitID != "" {
				return nil, fmt.Errorf("FindingSupplement original expression %s is unavailable: %w", row.Path, err)
			}
			break
		}
		var accepted map[string]any
		if err := json.Unmarshal(saved, &accepted); err != nil {
			if explicitID != "" {
				return nil, fmt.Errorf("decode original FindingSupplement: %w", err)
			}
			break
		}
		if !sameSupplementProfessionalInput(input, accepted) {
			if explicitID != "" {
				return nil, fmt.Errorf("FindingSupplement ID belongs to a different original expression")
			}
			break
		}
		return json.Marshal(accepted)
	}
	if explicitID == "" {
		input["supplement_id"] = generatedSupplementID(state, request.FindingID, input)
	}
	if _, ok := input["created_at"]; !ok {
		now := request.OccurredAt
		if now.IsZero() {
			now = time.Now().UTC()
		}
		input["created_at"] = now.UTC().Format(time.RFC3339Nano)
	}
	return json.Marshal(input)
}

// findingSupplementEventID names the append of one FindingSupplement by the
// supplement identity itself. A supplement is immutable and appended once, so
// its identity is the event identity; the Runtime write count is never part of
// it (G18 §1.1).
func findingSupplementEventID(supplementID string) string {
	if strings.TrimSpace(supplementID) == "" {
		return ""
	}
	return "evt-finding-supplement-" + supplementID
}

// findingSupplementIdempotencyKey names the append intent: the supplement
// identity is the responsibility coordinate and the intent key of this
// observation (G18 §1.3).
func findingSupplementIdempotencyKey(supplementID string) string {
	if strings.TrimSpace(supplementID) == "" {
		return ""
	}
	return "runtime:finding-supplement:" + supplementID
}

// generatedSupplementID names a supplement the author did not name. The
// baseline generation, the Finding it extends and the accepted observation are
// domain facts; the Runtime write count is not part of the identity
// (G18 §1.1, §1.2). The same observation therefore keeps one identity, and
// two different observations of the same Finding never collide.
func generatedSupplementID(state map[string]any, findingID string, input map[string]any) string {
	identity := map[string]any{"finding_id": findingID}
	for key, value := range input {
		switch key {
		case "supplement_id", "schema_version", "created_at":
			continue
		}
		identity[key] = value
	}
	encoded, err := json.Marshal(identity)
	if err != nil {
		encoded = []byte(findingID)
	}
	digest := sha256.Sum256(encoded)
	return fmt.Sprintf("supplement-g%d-%s", baselineGeneration(state), hex.EncodeToString(digest[:8]))
}

// publishSupplementArtifact only claims an empty path. Unknown existing files
// are neither read nor overwritten; the Writer records the chosen actual path.
func publishSupplementArtifact(root, relative string, data []byte) (string, error) {
	ext := filepath.Ext(relative)
	base := strings.TrimSuffix(relative, ext)
	for suffix := 0; ; suffix++ {
		candidate := relative
		if suffix > 0 {
			candidate = fmt.Sprintf("%s-%d%s", base, suffix, ext)
		}
		abs, err := repositoryContainedPath(root, candidate)
		if err != nil {
			return "", err
		}
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			return "", err
		}
		file, err := os.OpenFile(abs, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if errors.Is(err, os.ErrExist) {
			continue
		}
		if err != nil {
			return "", err
		}
		if _, err = file.Write(data); err != nil {
			_ = file.Close()
			_ = os.Remove(abs)
			return "", err
		}
		if err = file.Sync(); err != nil {
			_ = file.Close()
			return "", err
		}
		if err = file.Close(); err != nil {
			return "", err
		}
		if err = loopruntime.SyncDirectory(filepath.Dir(abs)); err != nil {
			return "", err
		}
		return candidate, nil
	}
}

func sameSupplementProfessionalInput(input, accepted map[string]any) bool {
	for key, want := range input {
		switch key {
		case "schema_version", "supplement_id", "supplements_finding_id", "author", "created_at":
			continue
		}
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
		case "schema_version", "supplement_id", "supplements_finding_id", "author", "created_at":
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

// applySupplement appends the current row and evidence in the Runtime transaction.
func applySupplement(state map[string]any, row SupplementRow, lens string, occurredAt time.Time) error {
	entities, ok := state["entities"].(map[string]any)
	if !ok {
		return fmt.Errorf("runtime entities must be an object")
	}
	rows, _ := entities["finding_supplements"].([]any)
	seen := make(map[string]string, len(rows)+1)
	for _, raw := range rows {
		entity, _ := raw.(map[string]any)
		if entity != nil {
			seen[stringField(entity["supplement_id"])] = stringField(entity["supplements_finding_id"])
		}
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
		"status":              "valid",
		"baseline_generation": row.BaselineGeneration,
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

// SupplementsForFinding returns the current Runtime's rows for the Finding.
func SupplementsForFinding(_ string, statePath, findingID string) ([]SupplementRow, error) {
	data, err := os.ReadFile(statePath)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read runtime: %w", err)
	}
	var state map[string]any
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("decode runtime: %w", err)
	}
	return filterSupplementRows(supplementRowsFromState(state), findingID), nil
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
			ReviewPlanID:         stringField(entity["review_plan_id"]),
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
		"baseline_generation":    row.BaselineGeneration,
		"created_at":             row.CreatedAt,
		"appended_at":            row.AppendedAt,
	}
	if row.ReviewPlanID != "" {
		entity["review_plan_id"] = row.ReviewPlanID
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
