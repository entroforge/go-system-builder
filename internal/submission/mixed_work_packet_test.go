package submission

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/workpacket"
)

func TestS5SourceRoutesMixedWorkPacketsBySubtype(t *testing.T) {
	root := t.TempDir()
	packet := mixedS5TestPacket(t)
	packetRow := mixedS5TestPacketRow(t, packet)
	state := mixedS5TestState(
		map[string]any{"packet_id": "packet-design", "template_id": "s2-design-v1"},
		map[string]any{"packet_id": "packet-contract", "template_id": "s3-contract-v1"},
		map[string]any{"packet_id": "packet-task-planning", "template_id": "s4-task-planning-v1"},
		packetRow,
	)

	got, source, err := (&Service{Root: root}).source(context.Background(), state, packet.Binding.Owner, filepath.Join(root, filepath.FromSlash(packet.AnswerPath)))
	if err != nil {
		t.Fatalf("mixed S2/S3/S4/S5 registry lookup: %v", err)
	}
	if got.ID != packet.ID || source.Root != root || source.Path != packet.AnswerPath || source.Execution != nil {
		t.Fatalf("selected packet/source = %s/%+v, want exact S5 Main source %s/%s", got.ID, source, packet.ID, packet.AnswerPath)
	}
}

func TestS5SubtypeRoutingSkipsOnlyKnownNonS5Packets(t *testing.T) {
	for _, subtype := range []string{
		"s2-design-v1", "s3-contract-v1", "s4-task-planning-v1",
		"s7-review-result-v1", "s8-hypothesis-result-v1",
		"s9-plan-report-v1", "s9-repair-result-v1",
	} {
		t.Run(subtype, func(t *testing.T) {
			state := mixedS5TestState(map[string]any{"packet_id": "packet-other", "template_id": subtype})
			if !skipKnownNonS5WorkPacket(state, "packet-other") {
				t.Fatalf("registered non-S5 subtype %q was not routed away from the S5 reader", subtype)
			}
		})
	}

	for _, tc := range []struct {
		name      string
		row       map[string]any
		duplicate bool
	}{
		{name: "S5 remains with its typed reader", row: map[string]any{"packet_id": "packet-x", "template_id": s5DocumentReviewTemplateID}},
		{name: "unknown subtype remains fail closed", row: map[string]any{"packet_id": "packet-x", "template_id": "s10-acceptance-v1"}},
		{name: "missing subtype remains fail closed", row: map[string]any{"packet_id": "packet-x"}},
		{name: "malformed packet identity is not skipped", row: map[string]any{"template_id": "s2-design-v1"}},
		{name: "duplicate identity is not skipped", row: map[string]any{"packet_id": "packet-x", "template_id": "s2-design-v1"}, duplicate: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rows := []any{tc.row}
			if tc.duplicate {
				rows = append(rows, map[string]any{"packet_id": "packet-x", "template_id": "s5-document-review-v1"})
			}
			if skipKnownNonS5WorkPacket(mixedS5TestState(rows...), "packet-x") {
				t.Fatal("noncanonical, unknown, or S5 row was incorrectly skipped")
			}
		})
	}
}

func TestS5SourceDoesNotSkipUnknownOrMalformedS5Rows(t *testing.T) {
	root := t.TempDir()
	packet := mixedS5TestPacket(t)
	validRow := mixedS5TestPacketRow(t, packet)
	path := filepath.Join(root, filepath.FromSlash(packet.AnswerPath))

	for _, tc := range []struct {
		name string
		row  map[string]any
	}{
		{name: "unknown subtype", row: map[string]any{"packet_id": "packet-unknown", "template_id": "s10-acceptance-v1"}},
		{name: "malformed S5 packet", row: map[string]any{"packet_id": "packet-malformed", "template_id": s5DocumentReviewTemplateID, "binding": "not-an-object"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state := mixedS5TestState(tc.row, validRow)
			if _, _, err := (&Service{Root: root}).source(context.Background(), state, packet.Binding.Owner, path); err == nil || !strings.Contains(err.Error(), "issued packet cannot be read") {
				t.Fatalf("S5 source skipped a row that must reach its typed lookup: %v", err)
			}
		})
	}
}

func mixedS5TestPacket(t *testing.T) workpacket.Packet {
	t.Helper()
	packet, err := workpacket.BuildS5(workpacket.Binding{
		RuntimeID: "loop-mixed-s5", Generation: 1, StageEntry: 3,
		Owner: "reviewer-mixed", AssignmentID: "assignment-mixed",
		AssignmentSHA: strings.Repeat("a", 64), Responsibility: "DV-SPEC-CONSISTENCY", Attempt: 1,
	}, []workpacket.Reference{{
		ID: "REQ-001", Path: "docs/requirements/REQ-001.md", Version: "1.0.0", SHA256: strings.Repeat("b", 64),
	}})
	if err != nil {
		t.Fatalf("build S5 test packet: %v", err)
	}
	return packet
}

func mixedS5TestPacketRow(t *testing.T, packet workpacket.Packet) map[string]any {
	t.Helper()
	data, err := json.Marshal(packet)
	if err != nil {
		t.Fatal(err)
	}
	var row map[string]any
	if err := json.Unmarshal(data, &row); err != nil {
		t.Fatal(err)
	}
	return row
}

func mixedS5TestState(rows ...any) map[string]any {
	return map[string]any{
		"schema_version": "1.2.0",
		"configuration":  map[string]any{"optimization": map[string]any{"packet": true}},
		"entities":       map[string]any{"work_packets": rows},
	}
}
