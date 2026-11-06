package investigation

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"

	"github.com/entroforge/go-system-builder/internal/workpacket"
)

// The S8 answer packet binds the exact Case revision it was issued against
// (Binding.CaseRef / Binding.CaseRevision) and the route tuple and question
// it was issued for. The Case keeps an append-only revision chain: every
// revision carries the predecessor's revision, path and operation, so every
// revision between the issuance and the current one is still on disk.
//
// A fresh submission and an open-packet recovery must prove that whole
// immutable Case lineage. Comparing only the current projection is not
// enough: a route decision or a question registered in an intermediate
// revision can be undone in the current projection, and that restored
// projection would otherwise revive an old issuance.
//
// The lineage is read from the Case's own recorded revision and route
// coordinates. No revision count, round, digest, fingerprint or checksum
// decides it, and the intermediate revisions are compared against the facts
// the packet itself already binds.
//
// L4-workflow-convergence.md:185 — "原 Reviewer 的真实首派发可直接引用已发行原 view／answer";
// :189 — "本人事件从同一平台子会话的真实原任务选择";
// L3-S8-finding-investigation.md:445 — "Case 已因本人题意或来源变化而失效 … 新内容不能冒充旧答卷重放".
func verifyS8CaseLineageContinuity(root string, current s8CurrentCase, packet workpacket.HypothesisResultPacket) error {
	history, err := objectArrayAllowEmpty(current.document["revision_history"], "InvestigationCase.revision_history")
	if err != nil {
		return err
	}
	for _, entry := range history {
		revision := integerValueOrZero(entry["revision"])
		if revision <= packet.Binding.CaseRevision {
			continue
		}
		path := stringField(entry["path"])
		if path == "" {
			return fmt.Errorf("S8 packet Case lineage continuity is broken: revision %d has no recorded Case revision path", revision)
		}
		document, err := readS8CaseRevision(root, path)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(s8CaseRoute(document), packet.Inputs.CaseRoute) {
			return fmt.Errorf("S8 packet Case lineage continuity is broken: Case revision %d changed the route authority after this packet was issued at revision %d", revision, packet.Binding.CaseRevision)
		}
		question, err := s8CaseQuestion(document, packet.Binding.HypothesisID)
		if err != nil {
			return fmt.Errorf("S8 packet Case lineage continuity is broken: Case revision %d no longer carries the issued question: %v", revision, err)
		}
		if !sameS8QuestionMeaning(question, packet.Inputs.Hypothesis) {
			return fmt.Errorf("S8 packet Case lineage continuity is broken: Case revision %d changed the issued question after this packet was issued at revision %d", revision, packet.Binding.CaseRevision)
		}
	}
	return nil
}

// readS8CaseRevision reads one immutable Case revision. The revision files
// are kept by the Case Writer, so an issued packet's own revision and every
// revision after it stay readable.
func readS8CaseRevision(root, path string) (map[string]any, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("S8 Case revision path is required")
	}
	data, err := workpacket.ReadFile(root, path, hypothesisResultPacketReadLimit)
	if err != nil {
		return nil, fmt.Errorf("read S8 Case revision %s: %v", path, err)
	}
	var document map[string]any
	if err := json.Unmarshal(data, &document); err != nil {
		return nil, fmt.Errorf("decode S8 Case revision %s: %v", path, err)
	}
	return document, nil
}

// s8CaseQuestion reads one registered question in the same shape the packet
// binds, so an intermediate revision can be compared with the issued packet.
func s8CaseQuestion(document map[string]any, hypothesisID string) (workpacket.HypothesisResultQuestion, error) {
	hypothesis, err := uniqueHypothesis(document, hypothesisID)
	if err != nil {
		return workpacket.HypothesisResultQuestion{}, err
	}
	return workpacket.HypothesisResultQuestion{
		HypothesisID: hypothesisID, AssignmentID: stringField(hypothesis["assignment_id"]),
		Statement: stringField(hypothesis["statement"]), Invariant: stringField(hypothesis["invariant"]),
		Discriminator: stringField(hypothesis["discriminator"]),
		ExpectedOutcomes: workpacket.HypothesisExpectedOutcomes{
			Support: stringField(s8Map(hypothesis["expected_outcomes"])["support"]),
			Refute:  stringField(s8Map(hypothesis["expected_outcomes"])["refute"]),
		},
		SourceFindingIDs: stringSliceAnyToStrings(hypothesis["source_finding_ids"]),
		EvidenceRefs:     stringSliceAnyToStrings(hypothesis["evidence_refs"]),
		Status:           stringField(hypothesis["status"]),
	}, nil
}

// sameS8QuestionMeaning compares the question meaning an Investigator was
// actually asked. The question status is not part of it: a status records
// whether the question was answered, not what the question meant.
func sameS8QuestionMeaning(left, right workpacket.HypothesisResultQuestion) bool {
	return left.HypothesisID == right.HypothesisID && left.AssignmentID == right.AssignmentID &&
		left.Statement == right.Statement && left.Invariant == right.Invariant &&
		left.Discriminator == right.Discriminator && left.ExpectedOutcomes == right.ExpectedOutcomes &&
		sameS8StringList(left.SourceFindingIDs, right.SourceFindingIDs) &&
		sameS8StringList(left.EvidenceRefs, right.EvidenceRefs)
}

func sameS8StringList(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}
