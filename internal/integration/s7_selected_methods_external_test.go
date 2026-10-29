package integration_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/entroforge/go-system-builder/internal/assignment"
	"github.com/entroforge/go-system-builder/internal/review"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

func TestS7RegisteredAdditionalMethodReachesNativeViewAndFrozenRecovery(t *testing.T) {
	fixture := newCanonicalMainFixtureWithSetup(t, canonicalSourceFile, true, false, enablePacketModeAndApplyTR003)
	registeredPlan := registerCanonicalPlanForPacketTest(t, fixture)
	plan, _, err := review.LoadPlan(fixture.root, registeredPlan.State)
	if err != nil {
		t.Fatal(err)
	}
	var selectedID string
	for _, a := range plan.Assignments {
		if a.Lens == "qa" {
			selectedID = a.AssignmentID
			break
		}
	}
	if selectedID == "" {
		t.Fatal("fixture lacks QA responsibility")
	}
	manifest, _, err := review.DraftManifest(fixture.root, registeredPlan.State, selectedID)
	if err != nil {
		t.Fatal(err)
	}
	const extra = "database-change"
	for _, name := range manifest.Assignments[0].SkillRefs {
		if name == extra {
			t.Fatal("fixture requires an additional explicitly selected method")
		}
	}
	manifest.Assignments[0].SkillRefs = append(manifest.Assignments[0].SkillRefs, extra)
	owner := "agent-s7-selected-method"
	manifest.Assignments[0].AgentID = owner
	ref := ".claude/workgroups/s7-selected-method.json"
	path := filepath.Join(fixture.root, ref)
	writeJSONFixture(t, path, manifest)
	registered, err := assignment.Register(fixture.root, fixture.statePath, fixture.journalPath, assignment.Request{ExpectedRevision: registeredPlan.Revision, ManifestPath: path, TaskID: canonicalTaskID, TaskPath: filepath.Join(fixture.root, canonicalTask), OccurredAt: time.Now().UTC()})
	if err != nil {
		t.Fatal("original Register rejected explicitly selected method", err)
	}
	manifestBytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	foundSource := false
	for _, raw := range registered.State["entities"].(map[string]any)["teams"].([]any) {
		team := raw.(map[string]any)
		if team["id"] == manifest.WorkgroupID {
			foundSource = team["manifest_sha256"] == workpacket.Digest(manifestBytes)
		}
	}
	if !foundSource {
		t.Fatal("original registration did not pin selected methods source")
	}
	packets, err := review.IssueResultPackets(fixture.root, fixture.statePath, fixture.journalPath, -1, owner)
	if err != nil || len(packets) != 1 {
		t.Fatalf("issue original selected responsibility: %v (%d)", err, len(packets))
	}
	packet := packets[0]
	if packet.Inputs.MethodSource == nil || packet.Inputs.MethodSource.ManifestRef != ref || packet.Inputs.MethodSource.SHA256 != workpacket.Digest(manifestBytes) {
		t.Fatal("packet lost original manifest source")
	}
	viewBytes, err := os.ReadFile(filepath.Join(fixture.root, packet.ViewPath))
	if err != nil {
		t.Fatal(err)
	}
	var view workpacket.ReviewResultPacket
	if err := json.Unmarshal(viewBytes, &view); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, item := range view.Draft.Read {
		if item.Path == ".claude/skills/database-change/SKILL.md" && item.Purpose == workpacket.ReviewMethodPurpose(extra) {
			found = true
		}
	}
	if !found {
		t.Fatal("registered additional method was not delivered through native view.read")
	}
	answer := s7MechanismAnswer(t, fixture, packet)
	writeJSONFixture(t, filepath.Join(fixture.root, packet.AnswerPath), answer)
	author, err := os.ReadFile(filepath.Join(fixture.root, packet.AnswerPath))
	if err != nil {
		t.Fatal(err)
	}
	before := readRuntimeSnapshot(t, fixture.statePath, fixture.journalPath)
	if err := os.Remove(filepath.Join(fixture.root, packet.ViewPath)); err != nil {
		t.Fatal(err)
	}
	restored, err := review.IssueResultPackets(fixture.root, fixture.statePath, fixture.journalPath, -1, owner)
	if err != nil || len(restored) != 1 || !reflect.DeepEqual(restored[0], packet) {
		t.Fatal("re-observation refreshed original method selection/identity", err)
	}
	after := readRuntimeSnapshot(t, fixture.statePath, fixture.journalPath)
	restoredView, _ := os.ReadFile(filepath.Join(fixture.root, packet.ViewPath))
	restoredAuthor, _ := os.ReadFile(filepath.Join(fixture.root, packet.AnswerPath))
	if after.Revision != before.Revision || !bytes.Equal(viewBytes, restoredView) || !bytes.Equal(author, restoredAuthor) {
		t.Fatal("native recovery rewrote issuance or author's professional answer")
	}
	request := review.IssuedSubmitRequest{Context: t.Context(), ExpectedRevision: -1, PacketID: packet.ID, AnswerPath: packet.AnswerPath, OwnerAgentID: owner, SourceRoot: fixture.root}
	accepted, err := review.SubmitIssuedResult(fixture.root, fixture.statePath, fixture.journalPath, request)
	if err != nil {
		t.Fatal("selected method answer failed original consumer", err)
	}
	replay, err := review.SubmitIssuedResult(fixture.root, fixture.statePath, fixture.journalPath, request)
	if err != nil || replay.Revision != accepted.Revision {
		t.Fatal("selected method answer failed exact replay", err)
	}
	preserved, _ := os.ReadFile(filepath.Join(fixture.root, packet.AnswerPath))
	if !bytes.Equal(author, preserved) {
		t.Fatal("consumer rewrote original author bytes")
	}
}
