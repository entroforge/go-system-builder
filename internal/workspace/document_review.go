package workspace

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"

	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/schema"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

const DocumentReviewPurpose = "document_review"

// ReviewExecution resolves the declared source even while preparing or
// retired. Its presence revokes shared Main authoring; only ready is usable.
func ReviewExecution(state map[string]any, p workpacket.Packet) (Execution, bool, error) {
	b, err := Decode(state)
	if err != nil || b == nil {
		return Execution{}, p.Binding.ExecutionRequired, err
	}
	var selected Execution
	found := false
	for _, e := range b.AllExecutions() {
		if e.AssignmentID != p.Binding.AssignmentID || e.RuntimeID != p.Binding.RuntimeID || e.BaselineGeneration != p.Binding.Generation || e.WorkPacketID != p.ID {
			continue
		}
		if found && e.Generation < selected.Generation {
			continue
		}
		if found && e.Generation == selected.Generation {
			return e, true, fmt.Errorf("multiple executions claim the same document review generation")
		}
		bound, err := DocumentReviewPacket(state, e)
		if err != nil || bound.ID != p.ID {
			return e, true, fmt.Errorf("declared execution does not match the issued review: %v", err)
		}
		selected, found = e, true
	}
	if !found {
		if e, exists := b.Executions[p.Binding.AssignmentID]; exists && e.RuntimeID == p.Binding.RuntimeID && e.BaselineGeneration == p.Binding.Generation {
			return e, true, fmt.Errorf("declared execution does not match the issued review")
		}
	}
	return selected, found || p.Binding.ExecutionRequired, nil
}

func (b *ExecutionRegistry) PlanDocumentReview(ctx context.Context, state map[string]any, p workpacket.Packet, inputPaths []string) (Execution, error) {
	if !runtime.PacketEnabled(state) {
		return Execution{}, fmt.Errorf("document review execution requires enabled v1.2 packets")
	}
	issued, ok, err := runtime.FindWorkPacket(state, p.ID)
	if err != nil || !ok || !reflect.DeepEqual(issued, p) {
		return Execution{}, fmt.Errorf("document review execution requires the exact issued packet")
	}
	if _, accepted, err := runtime.FindSubmission(state, p.ID); err != nil {
		return Execution{}, err
	} else if accepted {
		return Execution{}, fmt.Errorf("document review was already received; restore its receipt instead of preparing another execution")
	}
	life, _ := state["lifecycle"].(map[string]any)
	if life["state"] != "document_verification" || intValue(life["phase_revision"]) != p.Binding.StageEntry {
		return Execution{}, fmt.Errorf("document reviewer dispatch belongs to an old S5 entry")
	}
	for _, ref := range p.Inputs {
		inputPaths = append(inputPaths, ref.Path)
	}
	analysisInputs, err := workpacket.TaskAnalysisInputs(b.MainRoot, p)
	if err != nil {
		return Execution{}, err
	}
	for path := range analysisInputs {
		inputPaths = append(inputPaths, path)
	}
	inputPaths = append(inputPaths, p.ViewPath)
	e, err := b.Plan(ctx, state, p.Binding.AssignmentID, p.Binding.Owner, []string{}, []string{}, inputPaths)
	if err != nil {
		return e, err
	}
	if existing, ok := b.Executions[e.AssignmentID]; ok && (existing.Purpose != DocumentReviewPurpose || existing.WorkPacketID != p.ID || existing.StageEntry == nil || *existing.StageEntry != p.Binding.StageEntry) {
		return e, fmt.Errorf("existing execution does not match this immutable document review")
	}
	e.Purpose = DocumentReviewPurpose
	e.WorkPacketID = p.ID
	entry := p.Binding.StageEntry
	e.StageEntry = &entry
	return e, nil
}

// DocumentReviewPacket checks the actual candidate registry. It performs no
// Writer mutation and is also used by the locked domain authorization callback.
func DocumentReviewPacket(state map[string]any, e Execution) (workpacket.Packet, error) {
	if !runtime.PacketEnabled(state) || e.Purpose != DocumentReviewPurpose || e.WorkPacketID == "" || e.StageEntry == nil || len(e.WritePaths) != 0 || len(e.Checks) != 0 {
		return workpacket.Packet{}, fmt.Errorf("execution is not a read-only issued document review")
	}
	p, ok, err := runtime.FindWorkPacket(state, e.WorkPacketID)
	if err != nil || !ok {
		return p, fmt.Errorf("execution's document review packet is unavailable: %v", err)
	}
	if p.Binding.RuntimeID != e.RuntimeID || p.Binding.Generation != e.BaselineGeneration || p.Binding.AssignmentID != e.AssignmentID || p.Binding.Owner != e.AgentID || p.Binding.StageEntry != *e.StageEntry {
		return p, fmt.Errorf("execution's document review binding changed")
	}
	return p, nil
}

func (b *ExecutionRegistry) ValidateDocumentReviewView(e Execution, p workpacket.Packet) error {
	submissionPaths, err := b.WorkerSubmissionPaths(e)
	if err != nil {
		return err
	}
	v, err := workpacket.BindExecution(p, e.Path, e.Generation, e.AgentID, submissionPaths.Answer)
	if err != nil {
		return err
	}
	// The re-derived view supplies the identity coordinates only. Its bytes are
	// never compared with the Worker's own view file.
	expected, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	var coordinates workpacket.ExecutionView
	if err := json.Unmarshal(expected, &coordinates); err != nil {
		return err
	}
	data, err := workpacket.ReadFile(e.Path, workpacket.WorkerViewPath, 1<<20)
	if err != nil {
		return err
	}
	// The Worker view must still be a valid, schema-conforming questionnaire.
	if err := schema.NewEmbeddedValidator().ValidateBytes("work-packet-execution.schema.json", data); err != nil {
		return fmt.Errorf("Worker view is not a valid execution questionnaire: %w", err)
	}
	var observed workpacket.ExecutionView
	if err := json.Unmarshal(data, &observed); err != nil {
		return fmt.Errorf("Worker view is not a readable execution questionnaire: %w", err)
	}
	if observed.Root != coordinates.Root || observed.Generation != coordinates.Generation || observed.Owner != coordinates.Owner || observed.AuthoringPath != coordinates.AuthoringPath {
		return fmt.Errorf("Worker view does not match the registered packet/execution")
	}
	if observed.Packet.ID != p.ID || observed.Packet.TemplateID != p.TemplateID || !reflect.DeepEqual(observed.Packet.Binding, p.Binding) {
		return fmt.Errorf("Worker view does not match the registered packet/execution")
	}
	// Input paths identify the materials supplied to the original review.
	// Their historic digest fields are not an authorization or acceptance
	// condition. The view itself is judged the same way: only its schema
	// validity and its original identity coordinates qualify it. Reviewers and
	// Main assess the actual material they consume.
	return nil
}

func intValue(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case float64:
		return int(n)
	case json.Number:
		i, _ := n.Int64()
		return int(i)
	}
	return -1
}
