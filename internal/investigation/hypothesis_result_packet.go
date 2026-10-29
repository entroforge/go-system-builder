package investigation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/entroforge/go-system-builder/internal/evidence"
	"github.com/entroforge/go-system-builder/internal/workstate"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/entroforge/go-system-builder/internal/controlproof"
	loopruntime "github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/schema"
	"github.com/entroforge/go-system-builder/internal/semantic"
	"github.com/entroforge/go-system-builder/internal/team"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

const hypothesisResultPacketReadLimit int64 = 20 << 20

// IssuedHypothesisResultRequest is the S8 adapter envelope for an authored
// answer. The original Runtime packet binds OwnerAgentID and SourceRoot;
// AuthorizeSource remains an optional integration callback.
type IssuedHypothesisResultRequest struct {
	Context          context.Context `json:"-"`
	ExpectedRevision int
	PacketID         string
	AnswerPath       string
	OwnerAgentID     string
	SourceRoot       string
	AuthorizeSource  func(root string, state map[string]any) error
	OccurredAt       time.Time
}

// IssueHypothesisResultPackets issues questions for actual, registered,
// open Investigator assignments through the existing Runtime work_packets
// index and original PACKET-ISSUE writer.
func IssueHypothesisResultPackets(root, statePath, journalPath string, expectedRevision int, ownerFilter string) ([]workpacket.HypothesisResultPacket, error) {
	return IssueHypothesisResultPacketsContext(context.Background(), root, statePath, journalPath, expectedRevision, ownerFilter)
}

func IssueHypothesisResultPacketsContext(ctx context.Context, root, statePath, journalPath string, expectedRevision int, ownerFilter string) ([]workpacket.HypothesisResultPacket, error) {
	writer := loopruntime.NewWriter(statePath, journalPath, root, semantic.RuntimeCandidateValidator{}).WithContext(ctx)
	snapshot, err := writer.Snapshot()
	if err != nil {
		return nil, err
	}
	if expectedRevision >= 0 && snapshot.Revision != expectedRevision {
		return nil, loopruntime.ErrStaleRevision
	}
	if !loopruntime.PacketEnabled(snapshot.State) {
		return nil, errors.New("S8 packet issuance requires explicit Runtime v1.2/v1.3 packet mode")
	}
	if err := hypothesisResultLifecycle(snapshot.State); err != nil {
		return nil, err
	}
	current, err := readCurrentInvestigationCase(root, snapshot.State)
	if err != nil {
		return nil, err
	}
	registrations, err := resolveHypothesisDispatches(root, snapshot.State, current)
	if err != nil {
		return nil, err
	}
	all, err := loopruntime.HypothesisResultPackets(snapshot.State)
	if err != nil {
		return nil, err
	}
	issued := make([]workpacket.HypothesisResultPacket, 0, len(registrations))
	for _, registration := range registrations {
		if ownerFilter != "" && registration.owner != ownerFilter {
			continue
		}
		var existing *workpacket.HypothesisResultPacket
		for i := range all {
			b := all[i].Binding
			if b.RuntimeID == stringField(snapshot.State["runtime_id"]) && b.BaselineGeneration == baselineGenerationValue(snapshot.State) &&
				b.CaseID == current.document["case_id"] && b.HypothesisID == stringField(registration.task["hypothesis_id"]) && b.AssignmentID == registration.assignmentID && b.OwnerAgentID == registration.owner {
				if existing != nil {
					return nil, errors.New("S8 assignment has multiple original answer packets")
				}
				copyPacket := all[i]
				existing = &copyPacket
			}
		}
		if existing != nil {
			if _, accepted, err := loopruntime.VerifiedHypothesisResultReceipt(root, snapshot.State, existing.ID, registration.owner); err != nil {
				return nil, fmt.Errorf("verify accepted S8 result before recovering sibling packets: %w", err)
			} else if accepted {
				if err := loopruntime.VerifyHypothesisResultPacketIssue(root, snapshot.State, *existing); err != nil {
					return nil, err
				}
				// A completed exact packet no longer needs an authoring draft.
				// Skip only after the original immutable Case receipt was verified;
				// do not let a changed Hypothesis status masquerade as completion.
				continue
			}
		}
		packet, err := buildCurrentHypothesisResultPacket(root, snapshot.State, current, registration)
		if err != nil {
			return nil, err
		}
		if existing != nil {
			if err := verifyHypothesisResultPacketCurrent(root, snapshot.State, current, registration, *existing); err != nil {
				return nil, fmt.Errorf("existing S8 packet %s is stale: %w", existing.ID, err)
			}
			if err := loopruntime.VerifyHypothesisResultPacketIssue(root, snapshot.State, *existing); err != nil {
				return nil, err
			}
			if err := publishHypothesisResultPacket(root, snapshot.State, *existing); err != nil {
				return nil, err
			}
			issued = append(issued, *existing)
			continue
		}
		if err := validateS8ManifestCaseEquivalent(root, current, registration); err != nil {
			return nil, fmt.Errorf("new S8 packet registered Case source is not equivalent to the current question: %w", err)
		}
		if err := verifyHypothesisRegistrationBeforeIssue(root, snapshot.State, registration); err != nil {
			return nil, err
		}
		encoded, err := json.Marshal(packet)
		if err != nil {
			return nil, err
		}
		var row map[string]any
		if err := json.Unmarshal(encoded, &row); err != nil {
			return nil, err
		}
		life, _ := snapshot.State["lifecycle"].(map[string]any)
		cursor := map[string]any{"state": life["state"], "phase": life["phase"]}
		snapshot, err = writer.Update(snapshot.Revision, loopruntime.Mutation{
			EventID: packet.IssuanceEventID, TransitionID: "PACKET-ISSUE", Event: "work_packet_issued",
			Actor: "orchestrator", IdempotencyKey: "packet:" + packet.ID, RuntimeID: packet.Binding.RuntimeID,
			BaselineGeneration: packet.Binding.BaselineGeneration, From: cursor, To: cursor,
			RetainLastTransition: true, OccurredAt: time.Now().UTC(),
			Apply: func(state map[string]any) error {
				if err := hypothesisResultLifecycle(state); err != nil {
					return err
				}
				lockedCase, err := readCurrentInvestigationCase(root, state)
				if err != nil {
					return err
				}
				lockedDispatches, err := resolveHypothesisDispatches(root, state, lockedCase)
				if err != nil {
					return err
				}
				lockedRegistration, ok := findHypothesisDispatch(lockedDispatches, registration.assignmentID, registration.owner)
				if !ok {
					return errors.New("S8 registered Investigator dispatch changed before issue")
				}
				if err := validateS8ManifestCaseEquivalent(root, lockedCase, lockedRegistration); err != nil {
					return fmt.Errorf("S8 registered Case source changed before issue: %w", err)
				}
				lockedPacket, err := buildCurrentHypothesisResultPacket(root, state, lockedCase, lockedRegistration)
				if err != nil || !reflect.DeepEqual(packet, lockedPacket) {
					return fmt.Errorf("S8 original question sources changed before packet issue: %v", err)
				}
				if err := verifyHypothesisRegistrationBeforeIssue(root, state, lockedRegistration); err != nil {
					return err
				}
				if prior, found, err := loopruntime.FindHypothesisResultPacket(state, packet.ID); err != nil {
					return err
				} else if found {
					if !reflect.DeepEqual(prior, packet) {
						return errors.New("S8 packet identity conflict")
					}
					return errS8PacketReplay
				}
				priorPackets, err := loopruntime.HypothesisResultPackets(state)
				if err != nil {
					return err
				}
				for _, prior := range priorPackets {
					b := prior.Binding
					if b.RuntimeID == packet.Binding.RuntimeID && b.BaselineGeneration == packet.Binding.BaselineGeneration && b.CaseID == packet.Binding.CaseID && b.HypothesisID == packet.Binding.HypothesisID && b.AssignmentID == packet.Binding.AssignmentID {
						return errors.New("S8 hypothesis already has a different frozen issuance")
					}
				}
				entities, _ := state["entities"].(map[string]any)
				if entities == nil {
					return errors.New("Runtime entities are unavailable")
				}
				rows, _ := entities["work_packets"].([]any)
				entities["work_packets"] = append(rows, row)
				candidate, err := json.MarshalIndent(state, "", "  ")
				if err != nil {
					return err
				}
				if len(candidate) > (16<<20)-(8<<10) {
					return errors.New("S8 cumulative packet issuance exceeds the original Runtime checkpoint budget")
				}
				return nil
			},
		})
		if err != nil {
			latest, readErr := writer.Snapshot()
			if readErr != nil {
				return nil, err
			}
			prior, found, findErr := loopruntime.FindHypothesisResultPacket(latest.State, packet.ID)
			if findErr != nil || !found || !reflect.DeepEqual(prior, packet) {
				return nil, err
			}
			snapshot = latest
		}
		if err := loopruntime.VerifyHypothesisResultPacketIssue(root, snapshot.State, packet); err != nil {
			return nil, err
		}
		if err := publishHypothesisResultPacket(root, snapshot.State, packet); err != nil {
			return nil, err
		}
		issued = append(issued, packet)
	}
	return issued, nil
}

var errS8PacketReplay = errors.New("exact S8 packet issuance replay")

type s8CurrentCase struct {
	pointer  map[string]any
	document map[string]any
	bytes    []byte
	path     string
	sha      string
	revision int
	findings []string
}

type s8HypothesisDispatch struct {
	owner, assignmentID, teamID, taskID string
	manifestRef, manifestSHA            string
	taskRef, taskSHA                    string
	caseRef, caseSHA                    string
	caseRevision                        int
	manifest                            map[string]any
	assignment                          map[string]any
	task                                map[string]any
}

func hypothesisResultLifecycle(state map[string]any) error {
	life, _ := state["lifecycle"].(map[string]any)
	if life["state"] != "bug_resolution" || life["phase"] != "investigation" {
		return errors.New("S8 packet issuance requires lifecycle bug_resolution.investigation")
	}
	if !loopruntime.PacketEnabled(state) {
		return errors.New("S8 packet contract requires explicit Runtime v1.2/v1.3 packet mode")
	}
	if currentReviewRound(state) < 1 {
		return errors.New("S8 current review round is unavailable")
	}
	if _, err := baselineGeneration(state); err != nil {
		return err
	}
	return nil
}

func readCurrentInvestigationCase(root string, state map[string]any) (s8CurrentCase, error) {
	review, _ := state["review"].(map[string]any)
	registeredPointer, _ := review["investigation"].(map[string]any)
	caseID := stringField(registeredPointer["case_id"])
	pointer, err := mutableCasePointer(state, caseID, "hypothesis_result_submitted")
	if err != nil {
		return s8CurrentCase{}, err
	}
	if stringField(pointer["status"]) != "investigating" {
		return s8CurrentCase{}, errors.New("S8 packet requires the current Case to be investigating")
	}
	path := stringField(pointer["path"])
	sha := stringField(pointer["sha256"])
	if path == "" || sha == "" {
		return s8CurrentCase{}, errors.New("current InvestigationCase pointer is incomplete")
	}
	data, err := workpacket.ReadFile(root, path, hypothesisResultPacketReadLimit)
	if err != nil || workpacket.Digest(data) != sha {
		return s8CurrentCase{}, fmt.Errorf("current InvestigationCase bytes/SHA differ: %v", err)
	}
	if err := workpacket.ValidateUniqueJSON(data); err != nil {
		return s8CurrentCase{}, err
	}
	if err := schema.NewEmbeddedValidator().ValidateBytes("review-investigation-case.schema.json", data); err != nil {
		return s8CurrentCase{}, err
	}
	var document map[string]any
	if err := json.Unmarshal(data, &document); err != nil {
		return s8CurrentCase{}, err
	}
	revision := intFieldS8(pointer["revision"])
	if document["case_id"] != pointer["case_id"] || intFieldS8(document["revision"]) != revision || revision < 1 {
		return s8CurrentCase{}, errors.New("current InvestigationCase identity/revision differs from Runtime pointer")
	}
	if err := validateS8RuntimeCaseRoute(state, document); err != nil {
		return s8CurrentCase{}, err
	}
	findings, err := stringSlice(document["source_finding_ids"], "InvestigationCase.source_finding_ids")
	if err != nil || (len(findings) == 0 && stringField(document["observation_batch_id"]) != "") {
		return s8CurrentCase{}, errors.New("current InvestigationCase Finding set is invalid")
	}
	if len(findings) > 0 {
		if err := validateFindingArtifacts(root, state, findings); err != nil {
			return s8CurrentCase{}, err
		}
	}
	return s8CurrentCase{pointer: pointer, document: document, bytes: data, path: path, sha: sha, revision: revision, findings: findings}, nil
}

func resolveHypothesisDispatches(root string, state map[string]any, current s8CurrentCase) ([]s8HypothesisDispatch, error) {
	entities, _ := state["entities"].(map[string]any)
	agents, err := objectArray(entities["agents"], "entities.agents")
	if err != nil {
		return nil, err
	}
	teams, err := objectArray(entities["teams"], "entities.teams")
	if err != nil {
		return nil, err
	}
	tasks, err := objectArray(entities["tasks"], "entities.tasks")
	if err != nil {
		return nil, err
	}
	hypotheses, err := objectArray(current.document["hypotheses"], "InvestigationCase.hypotheses")
	if err != nil {
		return nil, err
	}
	known := map[string]map[string]any{}
	for _, hypothesis := range hypotheses {
		id := stringField(hypothesis["hypothesis_id"])
		if id == "" || known[id] != nil {
			return nil, errors.New("InvestigationCase has a missing or duplicate hypothesis identity")
		}
		known[id] = hypothesis
	}
	out := []s8HypothesisDispatch{}
	seenOwnerAssignment := map[string]bool{}
	seenAgents := map[string]bool{}
	for _, agent := range agents {
		owner := stringField(agent["id"])
		if owner == "" {
			return nil, errors.New("Runtime Agent has no identity")
		}
		if seenAgents[owner] {
			return nil, fmt.Errorf("duplicate Runtime Agent identity %s", owner)
		}
		seenAgents[owner] = true
		if agent["role"] != "investigator" || !oneOf(stringField(workstate.AgentActivity(agent)), "reading", "working", "blocked") {
			continue
		}
		teamID := stringField(agent["team_id"])
		teamRow, err := uniqueEntityByID(teams, teamID, "S8 Investigator Team")
		if err != nil {
			return nil, err
		}
		if teamRow == nil || teamRow["kind"] != "investigator" {
			continue
		}
		manifestRef := stringField(teamRow["manifest_ref"])
		manifestBytes, err := workpacket.ReadFile(root, manifestRef, hypothesisResultPacketReadLimit)
		if err != nil {
			return nil, fmt.Errorf("read registered S8 manifest %s: %w", manifestRef, err)
		}
		registeredManifestSHA := stringField(teamRow["manifest_sha256"])
		if registeredManifestSHA == "" || registeredManifestSHA != workpacket.Digest(manifestBytes) {
			return nil, fmt.Errorf("registered S8 Team %s manifest bytes differ from the original registration digest", teamID)
		}
		if err := schema.NewValidator(root).ValidateBytes("team-manifest.schema.json", manifestBytes); err != nil {
			return nil, fmt.Errorf("registered S8 manifest schema: %w", err)
		}
		if err := team.ValidateBytes(manifestBytes); err != nil {
			return nil, fmt.Errorf("registered S8 manifest semantics: %w", err)
		}
		var manifest map[string]any
		if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
			return nil, err
		}
		if manifest["runtime_id"] != state["runtime_id"] || manifest["workgroup_id"] != teamID || manifest["workgroup_kind"] != "investigator" || intFieldS8(manifest["baseline_generation"]) != intFieldS8(s8Map(state["baseline"])["generation"]) || intFieldS8(manifest["review_round"]) != currentReviewRound(state) {
			return nil, errors.New("registered S8 manifest Runtime/team/generation/round tuple differs")
		}
		caseRef, caseSHA, caseRevision, err := s8ManifestCaseReference(manifest)
		if err != nil {
			return nil, err
		}
		manifestAssignments, err := objectArray(manifest["assignments"], "team-manifest.assignments")
		if err != nil {
			return nil, err
		}
		var ownedAssignment map[string]any
		for _, assignment := range manifestAssignments {
			if assignment["agent_id"] == owner && assignment["role_family"] == "investigator" {
				if ownedAssignment != nil {
					return nil, fmt.Errorf("S8 Team %s gives Agent %s multiple Investigator Assignments", teamID, owner)
				}
				ownedAssignment = assignment
			}
		}
		if ownedAssignment == nil {
			continue
		}
		assignmentID := stringField(ownedAssignment["assignment_id"])
		agentTaskIDs, err := stringSlice(agent["task_ids"], "Agent.task_ids")
		if err != nil {
			return nil, err
		}
		var matchedTask map[string]any
		var matchedTaskDoc map[string]any
		var matchedTaskRef, matchedTaskSHA string
		for _, taskRow := range tasks {
			taskID := stringField(taskRow["id"])
			if !containsString(agentTaskIDs, taskID) || !containsStringAny(taskRow["owner_agent_ids"], owner) || stringField(taskRow["path"]) == "" || stringField(taskRow["sha256"]) == "" {
				continue
			}
			data, err := workpacket.ReadFile(root, stringField(taskRow["path"]), hypothesisResultPacketReadLimit)
			if err != nil || workpacket.Digest(data) != stringField(taskRow["sha256"]) {
				return nil, fmt.Errorf("registered S8 TASK %s bytes/SHA differ: %v", taskID, err)
			}
			if err := workpacket.ValidateUniqueJSON(data); err != nil {
				return nil, err
			}
			var task map[string]any
			if err := json.Unmarshal(data, &task); err != nil {
				return nil, err
			}
			if task["case_id"] != current.document["case_id"] || task["assignment_id"] != assignmentID {
				continue
			}
			hypothesisID := stringField(task["hypothesis_id"])
			hypothesis := known[hypothesisID]
			if hypothesis == nil || task["objective"] != hypothesis["discriminator"] {
				return nil, fmt.Errorf("registered S8 TASK %s does not bind the open Case question", taskID)
			}
			if matchedTask != nil {
				return nil, fmt.Errorf("S8 Assignment %s has multiple registered TASKs for one Case question", assignmentID)
			}
			matchedTask, matchedTaskDoc = taskRow, task
			matchedTaskRef, matchedTaskSHA = stringField(taskRow["path"]), stringField(taskRow["sha256"])
		}
		if matchedTask == nil {
			continue
		}
		if workstate.TaskActivity(matchedTask) != "in_progress" || !containsStringAny(matchedTask["owner_agent_ids"], owner) {
			return nil, fmt.Errorf("S8 TASK %s is not an active registered dispatch for Agent %s", stringField(matchedTask["id"]), owner)
		}
		if !scopeHasExactS8Assignment(ownedAssignment, stringField(current.document["case_id"]), stringField(matchedTaskDoc["hypothesis_id"])) {
			return nil, fmt.Errorf("S8 manifest Assignment %s scope differs from Case/Hypothesis", assignmentID)
		}
		if !uniqueManifestAssignment(manifestAssignments, assignmentID, owner, ownedAssignment) {
			return nil, fmt.Errorf("S8 manifest Assignment %s identity is ambiguous", assignmentID)
		}
		key := owner + "\x00" + assignmentID
		if seenOwnerAssignment[key] {
			return nil, fmt.Errorf("duplicate S8 dispatch %s/%s", owner, assignmentID)
		}
		seenOwnerAssignment[key] = true
		out = append(out, s8HypothesisDispatch{owner: owner, assignmentID: assignmentID, teamID: teamID,
			taskID: stringField(matchedTask["id"]), manifestRef: manifestRef, manifestSHA: workpacket.Digest(manifestBytes),
			taskRef: matchedTaskRef, taskSHA: matchedTaskSHA, caseRef: caseRef, caseSHA: caseSHA, caseRevision: caseRevision,
			manifest: manifest, assignment: ownedAssignment, task: matchedTaskDoc})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].assignmentID == out[j].assignmentID {
			return out[i].owner < out[j].owner
		}
		return out[i].assignmentID < out[j].assignmentID
	})
	return out, nil
}

func buildCurrentHypothesisResultPacket(root string, state map[string]any, current s8CurrentCase, dispatch s8HypothesisDispatch) (workpacket.HypothesisResultPacket, error) {
	hypothesisID := stringField(dispatch.task["hypothesis_id"])
	hypothesis, err := uniqueHypothesis(current.document, hypothesisID)
	if err != nil {
		return workpacket.HypothesisResultPacket{}, err
	}
	if hypothesis["status"] != "open" || stringField(hypothesis["assignment_id"]) != dispatch.assignmentID {
		return workpacket.HypothesisResultPacket{}, fmt.Errorf("S8 Assignment %s does not own an open Case Hypothesis", dispatch.assignmentID)
	}
	question := workpacket.HypothesisResultQuestion{
		HypothesisID: hypothesisID, AssignmentID: stringField(hypothesis["assignment_id"]),
		Statement: stringField(hypothesis["statement"]), Invariant: stringField(hypothesis["invariant"]), Discriminator: stringField(hypothesis["discriminator"]),
		ExpectedOutcomes: workpacket.HypothesisExpectedOutcomes{Support: stringField(s8Map(hypothesis["expected_outcomes"])["support"]), Refute: stringField(s8Map(hypothesis["expected_outcomes"])["refute"])},
		SourceFindingIDs: stringSliceAnyToStrings(hypothesis["source_finding_ids"]), EvidenceRefs: stringSliceAnyToStrings(hypothesis["evidence_refs"]), Status: stringField(hypothesis["status"]),
	}
	questionSHA, err := workpacket.HypothesisQuestionDigest(question)
	if err != nil {
		return workpacket.HypothesisResultPacket{}, err
	}
	caseRoute := s8CaseRoute(current.document)
	routeSHA, err := workpacket.HypothesisCaseRouteDigest(caseRoute)
	if err != nil {
		return workpacket.HypothesisResultPacket{}, err
	}
	var batch struct {
		ObservationBatchID string   `json:"observation_batch_id"`
		RuntimeID          string   `json:"runtime_id"`
		ReviewRound        int      `json:"review_round"`
		Generation         int      `json:"baseline_generation"`
		SubjectDigest      string   `json:"subject_digest"`
		FindingIDs         []string `json:"finding_ids"`
	}
	batchRef, batchSHA := stringField(current.document["observation_batch_ref"]), stringField(current.document["observation_batch_sha256"])
	if batchRef != "" {
		batchBytes, err := workpacket.ReadFile(root, batchRef, hypothesisResultPacketReadLimit)
		if err != nil || workpacket.Digest(batchBytes) != batchSHA {
			return workpacket.HypothesisResultPacket{}, fmt.Errorf("S8 original ObservationBatch bytes/SHA differ: %v", err)
		}
		if err := schema.NewEmbeddedValidator().ValidateBytes("observation-batch.schema.json", batchBytes); err != nil {
			return workpacket.HypothesisResultPacket{}, err
		}
		if err := json.Unmarshal(batchBytes, &batch); err != nil {
			return workpacket.HypothesisResultPacket{}, err
		}
		if batch.ObservationBatchID != stringField(current.document["observation_batch_id"]) || batch.RuntimeID != stringField(state["runtime_id"]) || batch.ReviewRound != currentReviewRound(state) || batch.Generation != intFieldS8(s8Map(state["baseline"])["generation"]) || batch.SubjectDigest != stringField(current.document["baseline_digest"]) || !sameStringSet(batch.FindingIDs, current.findings) {
			return workpacket.HypothesisResultPacket{}, errors.New("S8 ObservationBatch differs from current Case/source generation")
		}
	}
	findingRows := []workpacket.HypothesisResultFinding{}
	if len(current.findings) > 0 {
		findingRows, err = readS8FindingDocuments(root, state, current.findings)
		if err != nil {
			return workpacket.HypothesisResultPacket{}, err
		}
	}
	allRefs := append([]string(nil), question.EvidenceRefs...)
	refsToValidate := append([]string(nil), question.EvidenceRefs...)
	for _, finding := range findingRows {
		var doc struct {
			EvidenceRefs []string `json:"evidence_refs"`
		}
		if err := json.Unmarshal(finding.Document, &doc); err != nil {
			return workpacket.HypothesisResultPacket{}, err
		}
		allRefs = append(allRefs, doc.EvidenceRefs...)
		for _, ref := range doc.EvidenceRefs {
			_, typed, err := evidence.ParseFindingReference(ref)
			if err != nil {
				return workpacket.HypothesisResultPacket{}, fmt.Errorf("S8 original Finding %s source %q: %w", finding.FindingID, ref, err)
			}
			if typed {
				if err := evidence.ValidateFindingReference(root, ref); err != nil {
					return workpacket.HypothesisResultPacket{}, fmt.Errorf("S8 original Finding %s source %q: %w; Main restores the accepted source for the original question; do not create an evidence alias", finding.FindingID, ref, err)
				}
			} else {
				refsToValidate = append(refsToValidate, ref)
			}
		}
	}
	if batchRef != "" {
		refsToValidate, err = remainingFindingQuestionRefs(root, state, current.document, question.SourceFindingIDs, refsToValidate)
		if err != nil {
			return workpacket.HypothesisResultPacket{}, err
		}
	}
	if batchRef == "" {
		source, err := humanCaseSource(root, state, current.document)
		if err != nil {
			return workpacket.HypothesisResultPacket{}, err
		}
		refsToValidate = withoutHumanSource(refsToValidate, source)
	}
	if len(refsToValidate) > 0 {
		if err := ValidateEvidenceRefs(refsToValidate, EvidenceAttestationOptions{AllowPaths: true, State: state, Root: root, RequireSHA: true, RequireReviewRound: currentReviewRound(state)}); err != nil {
			return workpacket.HypothesisResultPacket{}, fmt.Errorf("S8 frozen evidence source closure: %w", err)
		}
	}
	evidenceRefs, err := buildS8EvidenceRefTuples(state, allRefs, question.EvidenceRefs, findingRows, question.SourceFindingIDs)
	if err != nil {
		return workpacket.HypothesisResultPacket{}, err
	}
	assignmentBytes, err := json.Marshal(dispatch.assignment)
	if err != nil {
		return workpacket.HypothesisResultPacket{}, err
	}
	generation, err := baselineGeneration(state)
	if err != nil {
		return workpacket.HypothesisResultPacket{}, err
	}
	inputs := workpacket.HypothesisResultInputs{Hypothesis: question, CaseRoute: caseRoute, Findings: findingRows, EvidenceRefs: evidenceRefs}
	binding := workpacket.HypothesisResultBinding{
		RuntimeID: stringField(state["runtime_id"]), BaselineGeneration: generation, ReviewRound: currentReviewRound(state),
		CaseID: stringField(current.document["case_id"]), CaseRef: current.path, CaseSHA256: current.sha, CaseRevision: current.revision, CaseRouteSHA256: routeSHA,
		ObservationBatchID: batch.ObservationBatchID, ObservationBatchRef: batchRef, ObservationBatchSHA256: batchSHA, BaselineDigest: stringField(current.document["baseline_digest"]),
		SourceFindingIDs: append([]string{}, current.findings...), HypothesisID: hypothesisID, HypothesisQuestionSHA256: questionSHA,
		HypothesisSourceFindingIDs: append([]string{}, question.SourceFindingIDs...), AssignmentID: dispatch.assignmentID,
		AssignmentSHA256: workpacket.Digest(assignmentBytes), TeamID: dispatch.teamID, TaskID: dispatch.taskID, TaskRef: dispatch.taskRef, TaskSHA256: dispatch.taskSHA,
		ManifestRef: dispatch.manifestRef, ManifestSHA256: dispatch.manifestSHA, OwnerAgentID: dispatch.owner,
	}
	return workpacket.BuildHypothesisResult(binding, inputs)
}

func verifyHypothesisResultPacketCurrent(root string, state map[string]any, current s8CurrentCase, dispatch s8HypothesisDispatch, packet workpacket.HypothesisResultPacket) error {
	// Fresh submission and open-packet recovery must prove the full immutable
	// Case lineage, not just compare the current projection. Otherwise a route
	// or question changed in an intermediate revision could be restored and
	// revive an old issuance. Exact accepted receipt replay returns before this
	// fresh-work validator.
	if err := loopruntime.VerifyHypothesisResultContinuity(root, state, packet); err != nil {
		return fmt.Errorf("S8 original Case continuity: %w", err)
	}
	if err := validateS8ManifestCaseEquivalent(root, current, dispatch); err != nil {
		return fmt.Errorf("S8 registered manifest Case source no longer matches the active question: %w", err)
	}
	currentPacket, err := buildCurrentHypothesisResultPacket(root, state, current, dispatch)
	if err != nil {
		return err
	}
	old, now := packet.Binding, currentPacket.Binding
	old.CaseRef, now.CaseRef = "", ""
	old.CaseSHA256, now.CaseSHA256 = "", ""
	old.CaseRevision, now.CaseRevision = 0, 0
	if !reflect.DeepEqual(old, now) || !reflect.DeepEqual(packet.Inputs, currentPacket.Inputs) {
		return errors.New("S8 packet source/question/route/registration differs from the current Case")
	}
	return nil
}

// validateS8ManifestCaseEquivalent permits an Investigator manifest to pin a
// prior immutable Case revision after a sibling answer advances the active
// pointer. It compares only the dispatch authority this assignment consumes:
// the same Case/batch/source Finding set, this still-open question, and the
// same eligible route tuple. The packet itself binds the exact active Case at
// its own issuance revision.
func validateS8ManifestCaseEquivalent(root string, current s8CurrentCase, dispatch s8HypothesisDispatch) error {
	pinnedBytes, err := workpacket.ReadFile(root, dispatch.caseRef, hypothesisResultPacketReadLimit)
	if err != nil || workpacket.Digest(pinnedBytes) != dispatch.caseSHA {
		return fmt.Errorf("registered Case %s bytes/SHA differ: %v", dispatch.caseRef, err)
	}
	if err := schema.NewEmbeddedValidator().ValidateBytes("review-investigation-case.schema.json", pinnedBytes); err != nil {
		return err
	}
	var pinned map[string]any
	if err := json.Unmarshal(pinnedBytes, &pinned); err != nil {
		return err
	}
	if pinned["case_id"] != current.document["case_id"] || intFieldS8(pinned["revision"]) != dispatch.caseRevision ||
		pinned["baseline_generation"] != current.document["baseline_generation"] || pinned["baseline_digest"] != current.document["baseline_digest"] ||
		pinned["observation_batch_id"] != current.document["observation_batch_id"] || pinned["observation_batch_ref"] != current.document["observation_batch_ref"] ||
		pinned["observation_batch_sha256"] != current.document["observation_batch_sha256"] ||
		!sameStringSet(stringSliceAnyToStrings(pinned["source_finding_ids"]), current.findings) || stringField(pinned["status"]) != "investigating" {
		return errors.New("registered Case no longer shares the current Case/batch/Finding source tuple")
	}
	pinnedRoute, err := workpacket.HypothesisCaseRouteDigest(s8CaseRoute(pinned))
	if err != nil {
		return err
	}
	currentRoute, err := workpacket.HypothesisCaseRouteDigest(s8CaseRoute(current.document))
	if err != nil || pinnedRoute != currentRoute {
		return errors.New("registered Case route differs from the active eligible route")
	}
	pinnedQuestion, err := s8Question(pinned, stringField(dispatch.task["hypothesis_id"]))
	if err != nil {
		return err
	}
	currentQuestion, err := s8Question(current.document, stringField(dispatch.task["hypothesis_id"]))
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(pinnedQuestion, currentQuestion) || pinnedQuestion.Status != "open" || pinnedQuestion.AssignmentID != dispatch.assignmentID {
		return errors.New("registered Case no longer contains this exact open Hypothesis question")
	}
	return nil
}

func s8Question(document map[string]any, hypothesisID string) (workpacket.HypothesisResultQuestion, error) {
	hypothesis, err := uniqueHypothesis(document, hypothesisID)
	if err != nil {
		return workpacket.HypothesisResultQuestion{}, err
	}
	return workpacket.HypothesisResultQuestion{
		HypothesisID: hypothesisID, AssignmentID: stringField(hypothesis["assignment_id"]),
		Statement: stringField(hypothesis["statement"]), Invariant: stringField(hypothesis["invariant"]), Discriminator: stringField(hypothesis["discriminator"]),
		ExpectedOutcomes: workpacket.HypothesisExpectedOutcomes{Support: stringField(s8Map(hypothesis["expected_outcomes"])["support"]), Refute: stringField(s8Map(hypothesis["expected_outcomes"])["refute"])},
		SourceFindingIDs: stringSliceAnyToStrings(hypothesis["source_finding_ids"]), EvidenceRefs: stringSliceAnyToStrings(hypothesis["evidence_refs"]), Status: stringField(hypothesis["status"]),
	}, nil
}

func verifyHypothesisRegistrationBeforeIssue(root string, state map[string]any, dispatch s8HypothesisDispatch) error {
	data, err := controlproof.ReadJournalForState(root, state)
	if err != nil {
		return err
	}
	inspection, err := controlproof.InspectJournalData(data)
	if err != nil {
		return err
	}
	if err := controlproof.ValidateStateJournalPair(state, inspection); err != nil {
		return err
	}
	generation := intFieldS8(s8Map(state["baseline"])["generation"])
	found := 0
	for _, event := range inspection.Events {
		if event["transition_id"] != "ENTITY-REGISTER" || event["runtime_id"] != state["runtime_id"] || event["outcome"] != "committed" {
			continue
		}
		after := intFieldS8(event["after_revision"])
		if after < 1 || after > intFieldS8(state["revision"]) || intFieldS8(event["baseline_generation"]) != generation {
			continue
		}
		ids, _ := stringSlice(event["evidence_ids"], "ENTITY-REGISTER.evidence_ids")
		if containsString(ids, dispatch.manifestRef) && containsString(ids, dispatch.taskRef) {
			found++
		}
	}
	if found != 1 {
		return fmt.Errorf("S8 manifest/TASK has %d exact original registration events; want one", found)
	}
	return nil
}

func publishHypothesisResultPacket(root string, state map[string]any, packet workpacket.HypothesisResultPacket) error {
	if err := loopruntime.VerifyHypothesisResultPacketIssue(root, state, packet); err != nil {
		return err
	}
	view, err := json.MarshalIndent(packet, "", "  ")
	if err != nil {
		return err
	}
	view = append(view, '\n')
	if current, err := workpacket.PublishBounded(root, packet.ViewPath, view, hypothesisResultPacketReadLimit); err != nil {
		return err
	} else if !reflect.DeepEqual(current, view) {
		return errors.New("S8 packet view differs from immutable Runtime issuance")
	}
	sourceRoot, independent, err := hypothesisResultSourceRoot(root, state, packet, false)
	if err != nil {
		return err
	}
	if independent {
		return nil
	}
	draft, err := json.MarshalIndent(packet.Draft, "", "  ")
	if err != nil {
		return err
	}
	current, err := workpacket.PublishBounded(sourceRoot, packet.AnswerPath, append(draft, '\n'), hypothesisResultPacketReadLimit)
	if err != nil {
		return err
	}
	if _, issues := workpacket.ValidateHypothesisResultAnswer(packet, current); len(issues) != 0 && !reflect.DeepEqual(current, append(draft, '\n')) {
		// Preserve a pre-existing author draft. A later Save validates it against
		// the exact same immutable packet; issuance never overwrites user bytes.
		return nil
	}
	return nil
}

func hypothesisResultSourceRoot(root string, state map[string]any, packet workpacket.HypothesisResultPacket, validateLive bool, contexts ...context.Context) (string, bool, error) {
	parent := context.Background()
	if len(contexts) > 0 && contexts[0] != nil {
		parent = contexts[0]
	}
	main, err := workspace.Canonical(root)
	if err != nil {
		return "", false, err
	}
	registry, err := workspace.Decode(state)
	if err != nil {
		return "", false, err
	}
	if registry == nil {
		return main, false, nil
	}
	if err := registry.ValidateAuthority(state); err != nil {
		return "", false, err
	}
	if registry.MainRoot != main {
		return "", false, errors.New("S8 workspace authority Main root differs from the current Runtime root")
	}
	execution, declared := registry.Executions[packet.Binding.AssignmentID]
	if !declared {
		return main, false, nil
	}
	if execution.AssignmentID != packet.Binding.AssignmentID || execution.RuntimeID != packet.Binding.RuntimeID || execution.BaselineGeneration != packet.Binding.BaselineGeneration || execution.AgentID != packet.Binding.OwnerAgentID || execution.Status != "ready" || execution.Path == main {
		return "", true, errors.New("S8 original execution owner/status/root differs")
	}
	if validateLive {
		ctx, cancel := context.WithTimeout(parent, 3*time.Second)
		defer cancel()
		if err := registry.ValidateExecution(ctx, execution, false); err != nil {
			return "", true, err
		}
		path, err := workspace.Canonical(execution.Path)
		if err != nil || path != execution.Path {
			return "", true, errors.New("S8 registered execution path is not canonical")
		}
		return path, true, nil
	}
	if !filepath.IsAbs(execution.Path) || filepath.Clean(execution.Path) != execution.Path {
		return "", true, errors.New("S8 registered execution path is not a canonical absolute path")
	}
	return execution.Path, true, nil
}

func findHypothesisDispatch(rows []s8HypothesisDispatch, assignmentID, owner string) (s8HypothesisDispatch, bool) {
	for _, row := range rows {
		if row.assignmentID == assignmentID && row.owner == owner {
			return row, true
		}
	}
	return s8HypothesisDispatch{}, false
}

func uniqueEntityByID(rows []map[string]any, id, label string) (map[string]any, error) {
	var found map[string]any
	for _, row := range rows {
		if row["id"] != id {
			continue
		}
		if found != nil {
			return nil, fmt.Errorf("duplicate %s identity %s", label, id)
		}
		found = row
	}
	return found, nil
}

func s8ManifestCaseReference(manifest map[string]any) (string, string, int, error) {
	refs, err := objectArray(manifest["documents"], "team-manifest.documents")
	if err != nil {
		return "", "", 0, err
	}
	found := 0
	ref, sha, revision := "", "", 0
	for _, doc := range refs {
		if doc["id"] != "investigation-case" {
			continue
		}
		path := stringField(doc["path"])
		digest := stringField(doc["sha256"])
		version := stringField(doc["version"])
		if path == "" || digest == "" || !strings.HasPrefix(version, "r") {
			return "", "", 0, errors.New("registered S8 manifest has an incomplete InvestigationCase source")
		}
		n, parseErr := strconv.Atoi(strings.TrimPrefix(version, "r"))
		if parseErr != nil || n < 1 || version != fmt.Sprintf("r%d", n) {
			return "", "", 0, errors.New("registered S8 manifest Case version is not a positive revision")
		}
		ref, sha, revision = path, digest, n
		found++
	}
	if found != 1 {
		return "", "", 0, errors.New("registered S8 manifest must contain one exact InvestigationCase source")
	}
	return ref, sha, revision, nil
}

func uniqueHypothesis(document map[string]any, id string) (map[string]any, error) {
	hypotheses, err := objectArray(document["hypotheses"], "InvestigationCase.hypotheses")
	if err != nil {
		return nil, err
	}
	var found map[string]any
	for _, row := range hypotheses {
		if row["hypothesis_id"] != id {
			continue
		}
		if found != nil {
			return nil, fmt.Errorf("duplicate InvestigationCase Hypothesis %s", id)
		}
		found = row
	}
	if found == nil {
		return nil, fmt.Errorf("InvestigationCase Hypothesis %s is absent", id)
	}
	return found, nil
}

func readS8FindingDocuments(root string, state map[string]any, findingIDs []string) ([]workpacket.HypothesisResultFinding, error) {
	entities, _ := state["entities"].(map[string]any)
	rows, err := objectArray(entities["findings"], "entities.findings")
	if err != nil {
		return nil, err
	}
	byID := map[string]map[string]any{}
	for _, row := range rows {
		id := stringField(row["finding_id"])
		if id == "" || byID[id] != nil {
			return nil, errors.New("Runtime Finding identity is missing or duplicated")
		}
		byID[id] = row
	}
	result := make([]workpacket.HypothesisResultFinding, 0, len(findingIDs))
	for _, id := range findingIDs {
		row := byID[id]
		if row == nil {
			return nil, fmt.Errorf("S8 Case Finding %s has no Runtime source row", id)
		}
		ref, sha := stringField(row["path"]), stringField(row["sha256"])
		data, err := workpacket.ReadFile(root, ref, 4<<20)
		if err != nil || workpacket.Digest(data) != sha {
			return nil, fmt.Errorf("S8 Finding %s bytes/SHA differ: %v", id, err)
		}
		if err := schema.NewEmbeddedValidator().ValidateBytes("finding.schema.json", data); err != nil {
			return nil, err
		}
		var document map[string]any
		if err := json.Unmarshal(data, &document); err != nil || document["finding_id"] != id {
			return nil, fmt.Errorf("S8 Finding %s document identity differs", id)
		}
		result = append(result, workpacket.HypothesisResultFinding{FindingID: id, Ref: ref, SHA256: sha, Document: json.RawMessage(data)})
	}
	return result, nil
}

func buildS8EvidenceRefTuples(state map[string]any, refs, questionRefs []string, findings []workpacket.HypothesisResultFinding, selected []string) ([]workpacket.HypothesisEvidenceRef, error) {
	entries, _ := state["evidence"].([]any)
	byID := map[string]map[string]any{}
	for _, raw := range entries {
		entry, _ := raw.(map[string]any)
		if entry == nil {
			continue
		}
		id := stringField(entry["id"])
		if id == "" || byID[id] != nil {
			return nil, errors.New("S8 Runtime evidence index contains missing or duplicate ID")
		}
		byID[id] = entry
	}
	questionCount := len(questionRefs)
	result := make([]workpacket.HypothesisEvidenceRef, 0, len(refs))
	// The packet Input order is the Hypothesis list followed by Finding lists.
	// Resolve each tuple from its owning source rather than treating the ref
	// string as self-authenticating.
	appendTuple := func(ref, origin, sourceFinding string) error {
		if origin == "finding" {
			source, typed, err := evidence.ParseFindingReference(ref)
			if err != nil {
				return err
			}
			if typed {
				result = append(result, workpacket.HypothesisEvidenceRef{Ref: ref, Origin: origin, SourceFindingID: sourceFinding, Attestation: "finding_source", Path: source.Path, SHA256: source.SHA256})
				return nil
			}
		}
		if origin == "hypothesis" {
			for _, finding := range findings {
				chosen := false
				for _, id := range selected {
					if id == finding.FindingID {
						chosen = true
					}
				}
				if !chosen {
					continue
				}
				var doc struct {
					Refs []string `json:"evidence_refs"`
				}
				if err := json.Unmarshal(finding.Document, &doc); err != nil {
					return err
				}
				for _, original := range doc.Refs {
					source, typed, err := evidence.ParseFindingReference(ref)
					if err != nil {
						return err
					}
					if original == ref && typed {
						result = append(result, workpacket.HypothesisEvidenceRef{Ref: ref, Origin: origin, Attestation: "finding_source", Path: source.Path, SHA256: source.SHA256})
						return nil
					}
				}
			}
		}
		if origin == "hypothesis" && strings.HasPrefix(ref, "path:") {
			source, _, err := evidence.ParseFindingReference(ref)
			if err != nil {
				return err
			}
			result = append(result, workpacket.HypothesisEvidenceRef{Ref: ref, Origin: origin, Attestation: "file_source", Path: source.Path, SHA256: source.SHA256})
			return nil
		}
		id, indexed, err := evidence.ReferenceID(ref)
		if err != nil {
			return err
		}
		if !indexed {
			result = append(result, workpacket.HypothesisEvidenceRef{Ref: ref, Origin: origin, SourceFindingID: sourceFinding, Attestation: "execution_anchor"})
			return nil
		}
		entry := byID[id]
		if entry == nil {
			for _, raw := range entries {
				row, _ := raw.(map[string]any)
				if row["kind"] == "human_decision" && row["path"] == ref {
					entry = row
					break
				}
			}
		}
		if entry == nil {
			return fmt.Errorf("S8 evidence %q has no Runtime index source", ref)
		}
		gen, round := intFieldS8(entry["baseline_generation"]), intFieldS8(entry["review_round"])
		if gen < 1 || round < 1 {
			return fmt.Errorf("S8 evidence %q lacks generation/round metadata", ref)
		}
		invalidated := (*string)(nil)
		if raw, ok := entry["invalidated_by"].(string); ok && strings.TrimSpace(raw) != "" {
			value := raw
			invalidated = &value
		}
		result = append(result, workpacket.HypothesisEvidenceRef{Ref: ref, Origin: origin, SourceFindingID: sourceFinding, Attestation: "runtime_index",
			Kind: stringField(entry["kind"]), Path: stringField(entry["path"]), SHA256: stringField(entry["sha256"]), Status: stringField(entry["status"]),
			BaselineGeneration: &gen, ReviewRound: &round, InvalidatedBy: invalidated})
		return nil
	}
	for _, ref := range questionRefs {
		if err := appendTuple(ref, "hypothesis", ""); err != nil {
			return nil, err
		}
	}
	for _, finding := range findings {
		var doc struct {
			EvidenceRefs []string `json:"evidence_refs"`
		}
		if err := json.Unmarshal(finding.Document, &doc); err != nil {
			return nil, err
		}
		for _, ref := range doc.EvidenceRefs {
			if err := appendTuple(ref, "finding", finding.FindingID); err != nil {
				return nil, err
			}
		}
	}
	if questionCount > len(result) || len(result) != len(refs) {
		return nil, errors.New("S8 evidence reference projection length differs")
	}
	return result, nil
}

func s8CaseRoute(document map[string]any) workpacket.HypothesisResultCaseRoute {
	return workpacket.HypothesisResultCaseRoute{Status: stringField(document["status"]), Route: s8OptionalString(document["route"]), RouteReason: s8OptionalString(document["route_reason"]), RouteConsumedAt: s8OptionalString(document["route_consumed_at"]), RouteConsumer: s8OptionalString(document["route_consumer"])}
}

// validateS8RuntimeCaseRoute keeps fresh S8 reads bound to both original
// authorities: the immutable current Case bytes and the Runtime pointer that
// consumes them. ConsumeCaseRoute records its acknowledgement in Runtime, so
// a still-unmodified Case file must not make that consumed question writable.
func validateS8RuntimeCaseRoute(state, document map[string]any) error {
	review, _ := state["review"].(map[string]any)
	pointer, _ := review["investigation"].(map[string]any)
	if pointer == nil || stringField(pointer["case_id"]) != stringField(document["case_id"]) {
		return errors.New("S8 Runtime InvestigationCase pointer does not identify the current Case")
	}
	pointerDigest, err := workpacket.HypothesisCaseRouteDigest(s8CaseRoute(pointer))
	if err != nil {
		return fmt.Errorf("S8 Runtime Case route is not eligible for fresh work: %w", err)
	}
	documentDigest, err := workpacket.HypothesisCaseRouteDigest(s8CaseRoute(document))
	if err != nil {
		return fmt.Errorf("S8 Case route is not eligible for fresh work: %w", err)
	}
	if pointerDigest != documentDigest {
		return errors.New("S8 Runtime Case route tuple differs from the pinned Case bytes")
	}
	return nil
}

func s8OptionalString(value any) *string {
	text, ok := value.(string)
	if !ok {
		return nil
	}
	return &text
}

func scopeHasExactS8Assignment(assignment map[string]any, caseID, hypothesisID string) bool {
	scope, err := stringSlice(assignment["scope"], "manifest Assignment.scope")
	if err != nil || len(scope) != 2 {
		return false
	}
	return (scope[0] == "case:"+caseID && scope[1] == "hypothesis:"+hypothesisID) || (scope[1] == "case:"+caseID && scope[0] == "hypothesis:"+hypothesisID)
}

func uniqueManifestAssignment(rows []map[string]any, assignmentID, owner string, selected map[string]any) bool {
	count := 0
	for _, row := range rows {
		if row["assignment_id"] == assignmentID && row["agent_id"] == owner {
			count++
			if !reflect.DeepEqual(row, selected) {
				return false
			}
		}
	}
	return count == 1
}

func sameStringSet(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	a, b := append([]string(nil), left...), append([]string(nil), right...)
	sort.Strings(a)
	sort.Strings(b)
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func intFieldS8(value any) int {
	switch v := value.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	case json.Number:
		n, _ := v.Int64()
		return int(n)
	default:
		return 0
	}
}

func stringSliceAnyToStrings(value any) []string {
	values, _ := value.([]any)
	result := make([]string, 0, len(values))
	for _, raw := range values {
		if text, ok := raw.(string); ok {
			result = append(result, text)
		}
	}
	return result
}

func containsString(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

func findHypothesisPacketForTarget(state map[string]any, caseID, hypothesisID, assignmentID string) (workpacket.HypothesisResultPacket, bool, error) {
	packets, err := loopruntime.HypothesisResultPackets(state)
	if err != nil {
		return workpacket.HypothesisResultPacket{}, false, err
	}
	var found *workpacket.HypothesisResultPacket
	for i := range packets {
		b := packets[i].Binding
		if b.CaseID != caseID || b.HypothesisID != hypothesisID || b.AssignmentID != assignmentID {
			continue
		}
		if found != nil {
			return workpacket.HypothesisResultPacket{}, false, errors.New("multiple S8 packets bind the same Hypothesis Assignment")
		}
		copyPacket := packets[i]
		found = &copyPacket
	}
	if found == nil {
		return workpacket.HypothesisResultPacket{}, false, nil
	}
	return *found, true, nil
}

func s8Map(value any) map[string]any {
	row, _ := value.(map[string]any)
	return row
}
