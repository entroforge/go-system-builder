package runtime

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"

	"github.com/entroforge/go-system-builder/internal/controlproof"
	"github.com/entroforge/go-system-builder/internal/evidence"
	"github.com/entroforge/go-system-builder/internal/schema"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

const hypothesisResultPacketTemplateID = "s8-hypothesis-result-v1"

// HypothesisResultPackets reads the existing packet registry. It does not
// grant permission to answer or derive an investigation from packet inputs.
func HypothesisResultPackets(state map[string]any) ([]workpacket.HypothesisResultPacket, error) {
	entities, _ := state["entities"].(map[string]any)
	rawRows, exists := entities["work_packets"]
	if !exists || rawRows == nil {
		return []workpacket.HypothesisResultPacket{}, nil
	}
	rows, ok := rawRows.([]any)
	if !ok {
		return nil, fmt.Errorf("Runtime work_packets is not an array")
	}
	seen := map[string]bool{}
	packets := make([]workpacket.HypothesisResultPacket, 0)
	for i, raw := range rows {
		row, ok := raw.(map[string]any)
		id := stringValue(row["packet_id"])
		if !ok || id == "" || seen[id] {
			return nil, fmt.Errorf("invalid or duplicate Runtime work_packets[%d] identity", i)
		}
		seen[id] = true
		if row["template_id"] != hypothesisResultPacketTemplateID {
			continue
		}
		data, err := json.Marshal(row)
		if err != nil {
			return nil, err
		}
		if err := schema.NewEmbeddedValidator().ValidateBytes("hypothesis-result-work-packet.schema.json", data); err != nil {
			return nil, fmt.Errorf("S8 packet %s schema: %w", id, err)
		}
		var p workpacket.HypothesisResultPacket
		if err := json.Unmarshal(data, &p); err != nil {
			return nil, err
		}
		rebuilt, err := workpacket.RebuildHypothesisResult(p)
		if err != nil || !reflect.DeepEqual(p, rebuilt) || p.ID != id {
			return nil, fmt.Errorf("S8 packet %s differs from its canonical rebuild", id)
		}
		packets = append(packets, p)
	}
	if len(packets) > 0 && !PacketEnabled(state) {
		return nil, fmt.Errorf("S8 packets require the explicit v1.2 packet contract")
	}
	sort.Slice(packets, func(i, j int) bool { return packets[i].ID < packets[j].ID })
	return packets, nil
}

func FindHypothesisResultPacket(state map[string]any, id string) (workpacket.HypothesisResultPacket, bool, error) {
	if id == "" {
		return workpacket.HypothesisResultPacket{}, false, fmt.Errorf("S8 packet ID is required")
	}
	packets, err := HypothesisResultPackets(state)
	if err != nil {
		return workpacket.HypothesisResultPacket{}, false, err
	}
	for _, p := range packets {
		if p.ID == id {
			return p, true, nil
		}
	}
	return workpacket.HypothesisResultPacket{}, false, nil
}

// CurrentHypothesisResultPackets selects current issued responsibilities.
// Accepted rows remain selectable for exact save replay; a new write grant
// still requires an open hypothesis, unchanged route and original Case
// Assignment continuity. Generic PLAN_REPORT is not a Case answer prerequisite.
func CurrentHypothesisResultPackets(root string, state map[string]any, owner string) ([]workpacket.HypothesisResultPacket, error) {
	all, err := HypothesisResultPackets(state)
	if err != nil || len(all) == 0 {
		return all, err
	}
	review, _ := state["review"].(map[string]any)
	pointer, _ := review["investigation"].(map[string]any)
	if pointer == nil {
		return []workpacket.HypothesisResultPacket{}, nil
	}
	document, err := readCheckpointHypothesisCase(root, stringValue(pointer["path"]), stringValue(pointer["sha256"]))
	if err != nil {
		return nil, err
	}
	if document["case_id"] != pointer["case_id"] || !hypothesisJSONEqual(document["revision"], pointer["revision"]) {
		return nil, fmt.Errorf("current S8 Case pointer identity differs")
	}
	baseline, _ := state["baseline"].(map[string]any)
	generation, _ := integerField(baseline, "generation")
	round, _ := integerField(review, "round")
	entities, _ := state["entities"].(map[string]any)
	selected := make([]workpacket.HypothesisResultPacket, 0)
	seenSlots := map[string]bool{}
	for _, p := range all {
		b := p.Binding
		if owner != "" && b.OwnerAgentID != owner || b.RuntimeID != state["runtime_id"] ||
			b.BaselineGeneration != generation || b.ReviewRound != round || b.CaseID != document["case_id"] {
			continue
		}
		var current map[string]any
		for _, row := range objectRows(document["hypotheses"]) {
			if row["hypothesis_id"] == b.HypothesisID {
				if current != nil {
					return nil, fmt.Errorf("current Case has duplicate hypothesis identity")
				}
				current = row
			}
		}
		if current == nil || current["assignment_id"] != b.AssignmentID {
			continue
		}
		encoded, err := json.Marshal(current)
		if err != nil {
			return nil, err
		}
		var question workpacket.HypothesisResultQuestion
		if err := json.Unmarshal(encoded, &question); err != nil {
			return nil, err
		}
		digest, err := workpacket.HypothesisQuestionDigest(question)
		if err != nil || digest != b.HypothesisQuestionSHA256 || !hypothesisExactStringSet(document["source_finding_ids"], b.SourceFindingIDs) ||
			stringValue(document["observation_batch_id"]) != b.ObservationBatchID || stringValue(document["observation_batch_ref"]) != b.ObservationBatchRef ||
			stringValue(document["observation_batch_sha256"]) != b.ObservationBatchSHA256 || stringValue(document["baseline_digest"]) != b.BaselineDigest {
			continue
		}
		accepted := false
		for _, result := range objectRows(document["hypothesis_results"]) {
			receipt, _ := result["receipt"].(map[string]any)
			if result["hypothesis_id"] == b.HypothesisID && result["assignment_id"] == b.AssignmentID && receipt["packet_id"] == p.ID {
				accepted = true
			}
		}
		if !accepted {
			if question.Status != "open" || !hypothesisCaseRouteEqual(document, p.Inputs.CaseRoute) || !hypothesisCaseRouteEqual(pointer, p.Inputs.CaseRoute) {
				continue
			}
			if err := VerifyHypothesisResultContinuity(root, state, p); err != nil {
				return nil, err
			}
		}
		registered := false
		for _, agent := range objectRows(entities["agents"]) {
			if agent["id"] != b.OwnerAgentID {
				continue
			}
			if agent["role"] != "investigator" || agent["team_id"] != b.TeamID || agent["prompt_ref"] != b.ManifestRef+"#"+b.AssignmentID {
				continue
			}
			tasks, _ := checkpointStringSlice(agent["task_ids"])
			if !hypothesisStringContains(tasks, b.TaskID) {
				continue
			}
			for _, team := range objectRows(entities["teams"]) {
				if team["id"] == b.TeamID && team["kind"] == "investigator" && team["manifest_ref"] == b.ManifestRef && team["manifest_sha256"] == b.ManifestSHA256 {
					registered = true
				}
			}
		}
		if !registered {
			continue
		}
		if seenSlots[b.AssignmentID] {
			return nil, fmt.Errorf("current S8 responsibility has multiple compatible issuances")
		}
		seenSlots[b.AssignmentID] = true
		selected = append(selected, p)
	}
	return selected, nil
}

// VerifyHypothesisResultPacketIssue checks the original committed issue and
// frozen sources. Current owner/PLAN, route compatibility and professional
// evidence remain checks of the original investigation consumer.
func VerifyHypothesisResultPacketIssue(root string, state map[string]any, p workpacket.HypothesisResultPacket) error {
	data, err := controlproof.ReadJournalForState(root, state)
	if err != nil {
		return err
	}
	inspection, err := controlproof.InspectJournalData(data)
	if err != nil {
		return err
	}
	return verifyHypothesisResultPacketIssue(root, state, p, inspection)
}

func verifyHypothesisResultPacketIssue(root string, state map[string]any, p workpacket.HypothesisResultPacket, journal controlproof.JournalInspection) error {
	stored, found, err := FindHypothesisResultPacket(state, p.ID)
	if err != nil || !found || !reflect.DeepEqual(stored, p) {
		return fmt.Errorf("S8 packet is not the exact Runtime registry row: %v", err)
	}
	if err := controlproof.ValidateStateJournalPair(state, journal); err != nil {
		return fmt.Errorf("S8 original state/journal cursor: %w", err)
	}
	i, found := journal.EventIndex[p.IssuanceEventID]
	if !found {
		return fmt.Errorf("S8 packet has no original issuance event")
	}
	event := journal.Events[i]
	actor, _ := event["actor"].(map[string]any)
	from, _ := event["from"].(map[string]any)
	to, _ := event["to"].(map[string]any)
	generation, genErr := integerField(event, "baseline_generation")
	before, beforeErr := integerField(event, "before_revision")
	after, afterErr := integerField(event, "after_revision")
	revision, revErr := integerField(state, "revision")
	if event["event"] != "transition_committed" || event["outcome"] != "committed" ||
		event["transition_id"] != "PACKET-ISSUE" || event["idempotency_key"] != "packet:"+p.ID ||
		event["runtime_id"] != p.Binding.RuntimeID || actor["type"] != "orchestrator" || actor["id"] != "orchestrator" ||
		genErr != nil || generation != p.Binding.BaselineGeneration || beforeErr != nil || afterErr != nil || after != before+1 ||
		revErr != nil || after > revision || from["state"] != "bug_resolution" || from["phase"] != "investigation" || !reflect.DeepEqual(from, to) {
		return fmt.Errorf("S8 packet original issuance tuple differs")
	}
	return verifyHypothesisResultPacketSources(root, state, p, journal, after)
}

func verifyHypothesisResultPacketSources(root string, state map[string]any, p workpacket.HypothesisResultPacket, journal controlproof.JournalInspection, issuedAfter int) error {
	b := p.Binding
	entities, _ := state["entities"].(map[string]any)
	registeredTeam := 0
	for _, team := range objectRows(entities["teams"]) {
		if team["id"] != b.TeamID {
			continue
		}
		registeredTeam++
		if team["kind"] != "investigator" || team["manifest_ref"] != b.ManifestRef || team["manifest_sha256"] != b.ManifestSHA256 {
			return fmt.Errorf("S8 manifest SHA differs from its original registered Team authority")
		}
	}
	if registeredTeam != 1 {
		return fmt.Errorf("S8 frozen manifest has no unique original Team registration")
	}
	read := func(ref, sha, schemaName string) (map[string]any, error) {
		data, err := workpacket.ReadFile(root, ref, 20<<20)
		if err != nil || workpacket.Digest(data) != sha {
			return nil, fmt.Errorf("S8 original source %s SHA/bytes differ: %v", ref, err)
		}
		if err := workpacket.ValidateUniqueJSON(data); err != nil {
			return nil, err
		}
		if schemaName != "" {
			if err := schema.NewEmbeddedValidator().ValidateBytes(schemaName, data); err != nil {
				return nil, err
			}
		}
		var document map[string]any
		if err := json.Unmarshal(data, &document); err != nil || document == nil {
			return nil, fmt.Errorf("S8 source %s is not an object", ref)
		}
		return document, nil
	}
	caseDocument, err := read(b.CaseRef, b.CaseSHA256, "review-investigation-case.schema.json")
	if err != nil {
		return err
	}
	caseRev, caseRevErr := integerField(caseDocument, "revision")
	caseGen, caseGenErr := integerField(caseDocument, "baseline_generation")
	if caseDocument["case_id"] != b.CaseID || caseRevErr != nil || caseRev != b.CaseRevision ||
		caseGenErr != nil || caseGen != b.BaselineGeneration || stringValue(caseDocument["baseline_digest"]) != b.BaselineDigest ||
		stringValue(caseDocument["observation_batch_id"]) != b.ObservationBatchID || stringValue(caseDocument["observation_batch_ref"]) != b.ObservationBatchRef ||
		stringValue(caseDocument["observation_batch_sha256"]) != b.ObservationBatchSHA256 || !hypothesisJSONEqual(caseDocument["source_finding_ids"], b.SourceFindingIDs) {
		return fmt.Errorf("S8 frozen Case tuple differs")
	}
	var hypothesis map[string]any
	for _, raw := range objectRows(caseDocument["hypotheses"]) {
		if raw["hypothesis_id"] == b.HypothesisID {
			if hypothesis != nil {
				return fmt.Errorf("S8 frozen Case has duplicate hypothesis identity")
			}
			hypothesis = raw
		}
	}
	if hypothesis == nil || !hypothesisJSONEqual(hypothesis, p.Inputs.Hypothesis) {
		return fmt.Errorf("S8 frozen hypothesis differs from issued question")
	}
	route := map[string]any{}
	for _, key := range []string{"status", "route", "route_reason", "route_consumed_at", "route_consumer"} {
		route[key] = caseDocument[key]
	}
	if !hypothesisJSONEqual(route, p.Inputs.CaseRoute) {
		return fmt.Errorf("S8 frozen route tuple differs")
	}
	if b.ObservationBatchRef != "" {
		batch, err := read(b.ObservationBatchRef, b.ObservationBatchSHA256, "observation-batch.schema.json")
		if err != nil {
			return err
		}
		batchGen, batchGenErr := integerField(batch, "baseline_generation")
		batchRound, batchRoundErr := integerField(batch, "review_round")
		if batch["observation_batch_id"] != b.ObservationBatchID || batch["runtime_id"] != b.RuntimeID ||
			batchGenErr != nil || batchGen != b.BaselineGeneration || batchRoundErr != nil || batchRound != b.ReviewRound ||
			batch["subject_digest"] != b.BaselineDigest || !hypothesisExactStringSet(batch["finding_ids"], b.SourceFindingIDs) {
			return fmt.Errorf("S8 original ObservationBatch tuple differs")
		}
	}
	for _, finding := range p.Inputs.Findings {
		document, err := read(finding.Ref, finding.SHA256, "finding.schema.json")
		if err != nil {
			return err
		}
		var frozen map[string]any
		if err := json.Unmarshal(finding.Document, &frozen); err != nil || document["finding_id"] != finding.FindingID || !hypothesisJSONEqual(document, frozen) {
			return fmt.Errorf("S8 frozen Finding %s differs", finding.FindingID)
		}
	}
	for _, ref := range p.Inputs.EvidenceRefs {
		if ref.Attestation == "finding_source" || ref.Attestation == "file_source" {
			if err := evidence.ValidateFindingReference(root, ref.Ref); err != nil {
				return fmt.Errorf("S8 frozen Finding %s source %s: %w", ref.SourceFindingID, ref.Ref, err)
			}
			continue
		}
		if ref.Attestation != "runtime_index" {
			continue
		}
		data, err := workpacket.ReadFile(root, ref.Path, 20<<20)
		if err != nil || workpacket.Digest(data) != ref.SHA256 {
			return fmt.Errorf("S8 frozen indexed evidence %s bytes/SHA differ: %v", ref.Ref, err)
		}
	}
	task, err := read(b.TaskRef, b.TaskSHA256, "")
	if err != nil {
		return err
	}
	if task["task_id"] != b.TaskID || task["case_id"] != b.CaseID || task["hypothesis_id"] != b.HypothesisID || task["assignment_id"] != b.AssignmentID {
		return fmt.Errorf("S8 registered TASK source tuple differs")
	}
	manifest, err := read(b.ManifestRef, b.ManifestSHA256, "team-manifest.schema.json")
	if err != nil {
		return err
	}
	manifestGeneration, manifestGenErr := integerField(manifest, "baseline_generation")
	manifestRound, manifestRoundErr := integerField(manifest, "review_round")
	if manifest["runtime_id"] != b.RuntimeID || manifest["workgroup_id"] != b.TeamID || manifest["workgroup_kind"] != "investigator" ||
		manifestGenErr != nil || manifestGeneration != b.BaselineGeneration || manifestRoundErr != nil || manifestRound != b.ReviewRound {
		return fmt.Errorf("S8 registered manifest tuple differs")
	}
	registeredCase := 0
	for _, source := range objectRows(manifest["documents"]) {
		if source["id"] != "investigation-case" {
			continue
		}
		registeredCase++
		dispatchCase, err := read(stringValue(source["path"]), stringValue(source["sha256"]), "review-investigation-case.schema.json")
		if err != nil {
			return err
		}
		if dispatchCase["case_id"] != b.CaseID || stringValue(dispatchCase["observation_batch_id"]) != b.ObservationBatchID ||
			stringValue(dispatchCase["observation_batch_ref"]) != b.ObservationBatchRef || stringValue(dispatchCase["observation_batch_sha256"]) != b.ObservationBatchSHA256 ||
			stringValue(dispatchCase["baseline_digest"]) != b.BaselineDigest || !hypothesisJSONEqual(dispatchCase["baseline_generation"], b.BaselineGeneration) ||
			!hypothesisExactStringSet(dispatchCase["source_finding_ids"], b.SourceFindingIDs) || !hypothesisCaseRouteEqual(dispatchCase, p.Inputs.CaseRoute) {
			return fmt.Errorf("S8 dispatch Case relevant source closure differs")
		}
		matches := 0
		for _, registeredQuestion := range objectRows(dispatchCase["hypotheses"]) {
			if hypothesisJSONEqual(registeredQuestion, p.Inputs.Hypothesis) {
				matches++
			}
		}
		if matches != 1 {
			return fmt.Errorf("S8 dispatch Case has no unique equivalent registered question")
		}
	}
	if registeredCase != 1 {
		return fmt.Errorf("S8 manifest has no unique pinned investigation Case")
	}
	var assignment map[string]any
	for _, row := range objectRows(manifest["assignments"]) {
		if row["assignment_id"] == b.AssignmentID {
			if assignment != nil {
				return fmt.Errorf("S8 manifest has duplicate Assignment identity")
			}
			assignment = row
		}
	}
	assignmentBytes, err := json.Marshal(assignment)
	if err != nil || assignment == nil || workpacket.Digest(assignmentBytes) != b.AssignmentSHA256 || assignment["agent_id"] != b.OwnerAgentID || assignment["role_family"] != "investigator" ||
		!hypothesisJSONEqual(assignment["scope"], []string{"case:" + b.CaseID, "hypothesis:" + b.HypothesisID}) {
		return fmt.Errorf("S8 actual manifest Assignment differs")
	}
	registered := 0
	for _, event := range journal.Events {
		if event["transition_id"] != "ENTITY-REGISTER" {
			continue
		}
		after, err := integerField(event, "after_revision")
		if err != nil || after >= issuedAfter || event["runtime_id"] != b.RuntimeID {
			continue
		}
		refs, _ := checkpointStringSlice(event["evidence_ids"])
		if hypothesisStringContains(refs, b.ManifestRef) && hypothesisStringContains(refs, b.TaskRef) {
			before, beforeErr := integerField(event, "before_revision")
			gen, genErr := integerField(event, "baseline_generation")
			actor, _ := event["actor"].(map[string]any)
			from, _ := event["from"].(map[string]any)
			to, _ := event["to"].(map[string]any)
			if beforeErr != nil || after != before+1 || genErr != nil || gen != b.BaselineGeneration ||
				actor["type"] != "orchestrator" || actor["id"] != "orchestrator" ||
				event["event_id"] != fmt.Sprintf("evt-register-%s-r%d", b.TeamID, after) ||
				event["idempotency_key"] != fmt.Sprintf("runtime:register:%s:%d", stringValue(manifest["manifest_id"]), before) ||
				from["state"] != "bug_resolution" || from["phase"] != "investigation" || !hypothesisJSONEqual(from, to) {
				return fmt.Errorf("S8 original manifest/TASK registration tuple differs")
			}
			registered++
		}
	}
	if registered != 1 {
		return fmt.Errorf("S8 manifest/TASK has no unique original registration preceding issue")
	}
	return nil
}

func hypothesisJSONEqual(a, b any) bool {
	x, errA := json.Marshal(a)
	y, errB := json.Marshal(b)
	if errA != nil || errB != nil {
		return false
	}
	var nx, ny any
	if json.Unmarshal(x, &nx) != nil || json.Unmarshal(y, &ny) != nil {
		return false
	}
	return reflect.DeepEqual(nx, ny)
}

func objectRows(value any) []map[string]any {
	rows, _ := value.([]any)
	objects := make([]map[string]any, 0, len(rows))
	for _, raw := range rows {
		if row, ok := raw.(map[string]any); ok {
			objects = append(objects, row)
		}
	}
	return objects
}

func hypothesisStringContains(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}

func hypothesisExactStringSet(value any, expected []string) bool {
	actual, ok := checkpointStringSlice(value)
	if !ok || len(actual) != len(expected) {
		return false
	}
	seen := map[string]bool{}
	for _, id := range actual {
		if id == "" || seen[id] {
			return false
		}
		seen[id] = true
	}
	for _, id := range expected {
		if !seen[id] {
			return false
		}
	}
	return true
}

func hypothesisCaseRouteEqual(document map[string]any, expected workpacket.HypothesisResultCaseRoute) bool {
	route := map[string]any{}
	for _, key := range []string{"status", "route", "route_reason", "route_consumed_at", "route_consumer"} {
		route[key] = document[key]
	}
	return hypothesisJSONEqual(route, expected)
}
