package repair

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/entroforge/go-system-builder/internal/workpacket"
)

// TargetedReceiptFact is a direct projection of an already accepted original
// Targeted report. ImpactID is populated when the caller needs the report.
type TargetedReceiptFact struct {
	Ref                    ArtifactRef
	PerformingAssignmentID string
	Result                 string
	ImpactID               string
	// Original is populated only when this lookup already read the domain
	// report. It is an in-memory value, not another persisted projection.
	Original *TargetedReverification
}

// TargetedReceiptUnknown preserves an accepted reference whose original
// association or conclusion could not be recovered. It is not a new result.
type TargetedReceiptUnknown struct {
	Ref    ArtifactRef
	Reason string
}

type TargetedReceiptFacts struct {
	Rows    []TargetedReceiptFact
	Unknown []TargetedReceiptUnknown
}

func canonicalTargetedAssignment(id string) string {
	if strings.HasPrefix(id, manifestVerificationPrefix) {
		return verificationAssignmentPrefix + strings.TrimPrefix(id, manifestVerificationPrefix)
	}
	return id
}

// LoadTargetedReceiptFacts resolves only already accepted Targeted facts.
// Receipts use their stored owner and conclusion; the original report is read
// only when the caller needs its ImpactID. An incomplete receipt has no usable
// conclusion and cannot cause an unrelated original report to be opened.
func LoadTargetedReceiptFacts(ctx context.Context, root string, pointer map[string]any, wantedAssignment string, withImpact bool) (TargetedReceiptFacts, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	wanted := canonicalTargetedAssignment(strings.TrimSpace(wantedAssignment))
	var facts TargetedReceiptFacts
	for _, raw := range anySlice(pointer["targeted_reverification_artifacts"]) {
		if err := ctx.Err(); err != nil {
			return facts, err
		}
		row, _ := raw.(map[string]any)
		if row == nil {
			continue
		}
		ref := ArtifactRef{Path: stringField(row["path"])}
		id, result := stringField(row["performing_assignment_id"]), stringField(row["result"])
		if id == "" || result == "" {
			continue
		}
		if wanted != "" && canonicalTargetedAssignment(id) != wanted {
			continue
		}
		fact := TargetedReceiptFact{Ref: ref, PerformingAssignmentID: id, Result: result}
		if withImpact {
			data, err := workpacket.ReadFile(root, ref.Path, 20<<20)
			if err != nil {
				facts.Unknown = append(facts.Unknown, TargetedReceiptUnknown{Ref: ref, Reason: err.Error()})
				facts.Rows = append(facts.Rows, fact)
				continue
			}
			var report TargetedReverification
			if err := json.Unmarshal(data, &report); err != nil {
				facts.Unknown = append(facts.Unknown, TargetedReceiptUnknown{Ref: ref, Reason: fmt.Sprintf("decode accepted Targeted report: %v", err)})
				facts.Rows = append(facts.Rows, fact)
				continue
			}
			fact.ImpactID = report.ImpactID
			fact.Original = &report
		}
		if wanted != "" && canonicalTargetedAssignment(fact.PerformingAssignmentID) != wanted {
			continue
		}
		facts.Rows = append(facts.Rows, fact)
	}
	return facts, nil
}
