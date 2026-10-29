package runtime

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"sort"
	"strings"

	"github.com/entroforge/go-system-builder/internal/reviewmeta"
	"github.com/entroforge/go-system-builder/internal/schema"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

// verifyReviewResultPacketSources re-reads the original pinned Plan through
// the Runtime authority root. It proves that the packet's Assignment digest,
// ordered Claim IDs, full Claim source objects, and frozen subject objects all
// came from those exact schema-validated Plan bytes.
func verifyReviewResultPacketSources(root string, packet workpacket.ReviewResultPacket) error {
	b := packet.Binding
	planBytes, err := workpacket.ReadFile(root, b.ReviewPlanRef, reviewResultArtifactLimit)
	if err != nil {
		return fmt.Errorf("read original pinned S7 ReviewPlan: %w", err)
	}
	if sha256Bytes(planBytes) != b.ReviewPlanSHA256 {
		return errors.New("original pinned S7 ReviewPlan bytes differ from the packet digest")
	}
	if err := workpacket.ValidateUniqueJSON(planBytes); err != nil {
		return fmt.Errorf("original pinned S7 ReviewPlan has duplicate JSON keys: %w", err)
	}
	if err := schema.NewEmbeddedValidator().ValidateBytes("review-plan.schema.json", planBytes); err != nil {
		return fmt.Errorf("original pinned S7 ReviewPlan schema: %w", err)
	}
	var plan reviewResultPlanSource
	if err := json.Unmarshal(planBytes, &plan); err != nil {
		return fmt.Errorf("decode original pinned S7 ReviewPlan source: %w", err)
	}
	if plan.SchemaVersion != "1.0.0" || plan.ReviewPlanID != b.ReviewPlanID || plan.ReviewRound != b.ReviewRound || plan.BaselineGeneration != b.BaselineGeneration {
		return errors.New("original pinned S7 ReviewPlan identity differs from the packet")
	}

	var original *reviewmeta.PlanAssignment
	for i := range plan.Assignments {
		if plan.Assignments[i].AssignmentID != b.AssignmentID {
			continue
		}
		if original != nil {
			return errors.New("original pinned S7 ReviewPlan has duplicate Assignment IDs")
		}
		assignment := plan.Assignments[i]
		original = &assignment
	}
	if original == nil || !sameStrings(original.ClaimIDs, b.ClaimIDs) || hasDuplicateStrings(original.ClaimIDs) {
		return errors.New("original pinned S7 Assignment Claim IDs differ from the packet")
	}
	assignmentBytes, err := json.Marshal(original)
	if err != nil || sha256Bytes(assignmentBytes) != b.AssignmentSHA256 {
		return errors.New("original pinned S7 Assignment JSON digest differs from the packet")
	}

	claimsByID := make(map[string]reviewmeta.Claim, len(plan.Claims))
	for _, claim := range plan.Claims {
		if claim.ClaimID == "" {
			return errors.New("original pinned S7 ReviewPlan contains a malformed Claim")
		}
		if _, duplicate := claimsByID[claim.ClaimID]; duplicate {
			return errors.New("original pinned S7 ReviewPlan has duplicate Claim IDs")
		}
		claimsByID[claim.ClaimID] = claim
	}
	selectedClaims := make([]reviewmeta.Claim, 0, len(original.ClaimIDs))
	for _, id := range original.ClaimIDs {
		claim, found := claimsByID[id]
		if !found {
			return fmt.Errorf("original pinned S7 Assignment Claim %s is absent from the Plan", id)
		}
		selectedClaims = append(selectedClaims, claim)
	}
	selectedClaimBytes, err := json.Marshal(selectedClaims)
	if err != nil {
		return fmt.Errorf("encode original pinned S7 Claim selection: %w", err)
	}
	claimSource, err := canonicalRuntimeJSON(selectedClaimBytes)
	if err != nil {
		return fmt.Errorf("canonicalize original pinned S7 Claims: %w", err)
	}
	packetClaims, err := canonicalRuntimeJSON(packet.Inputs.Claims)
	if err != nil || !bytes.Equal(claimSource, packetClaims) {
		return errors.New("S7 packet Claim inputs differ from the original pinned Plan")
	}

	subjectBytes, err := json.Marshal(plan.FrozenSubjects)
	if err != nil {
		return fmt.Errorf("encode original pinned S7 frozen subjects: %w", err)
	}
	subjectSource, err := canonicalRuntimeJSON(subjectBytes)
	if err != nil {
		return fmt.Errorf("canonicalize original pinned S7 frozen subjects: %w", err)
	}
	packetSubjects, err := canonicalRuntimeJSON(packet.Inputs.FrozenSubjects)
	if err != nil || !bytes.Equal(subjectSource, packetSubjects) {
		return errors.New("S7 packet frozen subject inputs differ from the original pinned Plan")
	}
	return verifyReviewResultMethodSource(root, packet, original.Lens)
}

// The packet freezes selected paths at issuance. Historical verification reads
// the original manifest, never today's role profile/catalog and never rewrites
// the original packet when installed method sources subsequently move.
func verifyReviewResultMethodSource(root string, packet workpacket.ReviewResultPacket, lens string) error {
	source := packet.Inputs.MethodSource
	if source == nil {
		return nil
	} // pre-method issuance, with its original ID
	data, err := workpacket.ReadFile(root, source.ManifestRef, 4<<20)
	if err != nil || workpacket.Digest(data) != source.SHA256 {
		return errors.New("S7 original selected-method manifest is missing or differs from its frozen digest")
	}
	if err := workpacket.ValidateUniqueJSON(data); err != nil {
		return err
	}
	var manifest struct {
		RuntimeID          string `json:"runtime_id"`
		BaselineGeneration int    `json:"baseline_generation"`
		ReviewRound        int    `json:"review_round"`
		WorkgroupKind      string `json:"workgroup_kind"`
		Assignments        []struct {
			ID     string   `json:"assignment_id"`
			Owner  string   `json:"agent_id"`
			Role   string   `json:"role_family"`
			Claims []string `json:"claim_ids"`
			Skills []string `json:"skill_refs"`
		} `json:"assignments"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		return err
	}
	b := packet.Binding
	kind := map[string]string{"delivery": "delivery_verifier", "qa": "qa", "e2e": "e2e_browser"}[lens]
	role := map[string]string{"delivery": "delivery-verifier", "qa": "qa", "e2e": "e2e-tester"}[lens]
	if manifest.RuntimeID != b.RuntimeID || manifest.BaselineGeneration != b.BaselineGeneration || manifest.ReviewRound != b.ReviewRound || manifest.WorkgroupKind != kind {
		return errors.New("S7 original method source differs from the Runtime/round/lens")
	}
	found := false
	for _, item := range manifest.Assignments {
		if item.ID != b.AssignmentID {
			continue
		}
		claims, bound := append([]string(nil), item.Claims...), append([]string(nil), b.ClaimIDs...)
		sort.Strings(claims)
		sort.Strings(bound)
		if found || item.Owner != b.OwnerAgentID || item.Role != role || !reflect.DeepEqual(claims, bound) || item.Skills == nil || hasDuplicateStrings(item.Skills) || len(item.Skills) != len(packet.Inputs.Methods) {
			return errors.New("S7 original method source differs from the Assignment/owner/selected set")
		}
		found = true
		selected := map[string]bool{}
		for _, name := range item.Skills {
			selected[name] = true
		}
		for _, method := range packet.Inputs.Methods {
			parts := strings.Split(strings.TrimPrefix(method.Path, ".claude/"), "/")
			if len(parts) != 3 || parts[0] != "skills" || parts[2] != "SKILL.md" || !selected[parts[1]] || method.Purpose != workpacket.ReviewMethodPurpose(parts[1]) {
				return errors.New("S7 delivered method differs from the original manifest selection")
			}
			delete(selected, parts[1])
		}
	}
	if !found {
		return errors.New("S7 original method source omits the issued Assignment")
	}
	return nil
}

type reviewResultPlanSource struct {
	SchemaVersion      string                      `json:"schema_version"`
	ReviewPlanID       string                      `json:"review_plan_id"`
	ReviewRound        int                         `json:"review_round"`
	BaselineGeneration int                         `json:"baseline_generation"`
	FrozenSubjects     []reviewmeta.FrozenSubject  `json:"frozen_subjects"`
	Claims             []reviewmeta.Claim          `json:"claims"`
	Assignments        []reviewmeta.PlanAssignment `json:"assignments"`
}

func canonicalRuntimeJSON(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return nil, errors.New("empty JSON")
	}
	if err := workpacket.ValidateUniqueJSON(data); err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return nil, errors.New("trailing JSON value")
		}
		return nil, err
	}
	return json.Marshal(value)
}

func hasDuplicateStrings(values []string) bool {
	seen := make(map[string]bool, len(values))
	for _, value := range values {
		if seen[value] {
			return true
		}
		seen[value] = true
	}
	return false
}
