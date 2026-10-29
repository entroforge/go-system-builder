package hook

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"

	"github.com/entroforge/go-system-builder/internal/fileview"
	"github.com/entroforge/go-system-builder/internal/hookctx"
	"github.com/entroforge/go-system-builder/internal/qualitygate"
	loopruntime "github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/schema"
	"github.com/entroforge/go-system-builder/internal/transition"
)

// currentBuilderWorkComplete asks the original S6 consumer to validate the
// current Builder delivery. A generic completion ref, manifest status or
// Agent terminal label never substitutes for this proof.
func currentBuilderWorkComplete(root string, snapshot loopruntime.Snapshot, assignment *hookctx.AssignmentContext) bool {
	if assignment == nil || assignment.OwnerAgentID == "" || assignment.AssignmentID == "" || assignment.TaskID == "" {
		return false
	}
	if !requiresBuilderReceipt(assignment) {
		return false
	}
	if err := fileview.ValidateAuthority(root, snapshot.State); err != nil {
		return false
	}
	branch, err := fileview.DevelopmentRef(snapshot.State)
	if err != nil {
		return false
	}
	catalog, err := transition.LoadCatalog(root)
	if err != nil || catalog == nil || catalog.Definition == nil {
		return false
	}
	ref := "refs/heads/" + strings.TrimPrefix(branch, "refs/heads/")
	view, err := fileview.New(root, ref, catalog.Definition.FileSources)
	if err != nil {
		return false
	}
	request := qualitygate.BuilderReceiptRequest{
		Owner: assignment.OwnerAgentID, AssignmentID: assignment.AssignmentID,
		TaskID: assignment.TaskID, CompletionRef: assignment.CompletionRef,
		RequiredChecks: append([]string(nil), assignment.RequiredChecks...),
		DoneWhen:       append([]string(nil), assignment.DoneWhen...),
	}
	receipt := qualitygate.CurrentBuilderReceipt(qualitygate.Input{Root: root, Snapshot: snapshot, Files: view}, request)
	if err := view.Verify(); err != nil || !receipt.WorkComplete {
		return false
	}
	data, err := json.Marshal(receipt)
	if err != nil || schema.NewEmbeddedValidator().ValidateBytes("builder-receipt.schema.json", data) != nil {
		return false
	}
	currentData, err := json.Marshal(qualitygate.CurrentBuilderReceipt(qualitygate.Input{Root: root, Snapshot: snapshot, Files: view}, request))
	if err != nil || !bytes.Equal(data, currentData) || view.Verify() != nil {
		return false
	}
	latest, err := loopruntime.NewStore(
		filepath.Join(root, ".claude", "loop-state.json"),
		filepath.Join(root, ".claude", "loop-events.jsonl"),
	).Snapshot()
	return err == nil && latest.Revision == snapshot.Revision && latest.State["runtime_id"] == snapshot.State["runtime_id"]
}
