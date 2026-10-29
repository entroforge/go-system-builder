package cli

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/entroforge/go-system-builder/internal/fileview"
	"github.com/entroforge/go-system-builder/internal/hookctx"
	"github.com/entroforge/go-system-builder/internal/qualitygate"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/schema"
	"github.com/entroforge/go-system-builder/internal/transition"
)

func runTaskReceipt(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("runtime task-receipt", flag.ContinueOnError)
	fs.SetOutput(stderr)
	bindUsage(fs, "runtime task-receipt")
	root := fs.String("root", ".", "bound control or registered Worker root")
	owner := fs.String("agent-id", "", "registered Builder owner")
	id := fs.String("assignment-id", "", "original Builder assignment")
	if err := parseWorkspaceFlags(fs, args); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}
	fail := func(err error) int { fmt.Fprintln(stderr, err); return 1 }
	if *owner == "" || *id == "" {
		return fail(fmt.Errorf("task-receipt requires --agent-id and --assignment-id"))
	}
	store := runtime.NewStore(filepath.Join(*root, ".claude/loop-state.json"), filepath.Join(*root, ".claude/loop-events.jsonl"))
	snapshot, err := store.Snapshot()
	if err != nil {
		return fail(err)
	}
	loaded, err := hookctx.LoadFull(*root, *owner)
	if err != nil {
		return fail(err)
	}
	if loaded.PolicyContext.Agent != nil && loaded.PolicyContext.Agent.RepairAssignmentID != "" {
		return fail(fmt.Errorf("S9 repair completion belongs to its original RepairResult receipt, not the S6 Builder receipt"))
	}
	var request qualitygate.BuilderReceiptRequest
	for _, a := range loaded.Assignments {
		if a.AssignmentID != *id || a.OwnerAgentID != *owner {
			continue
		}
		if request.AssignmentID != "" {
			return fail(fmt.Errorf("ambiguous Builder assignment"))
		}
		switch a.RoleFamily {
		case "backend-builder", "frontend-builder", "test-builder":
		default:
			return fail(fmt.Errorf("task-receipt requires the original Builder responsibility; use its domain receipt for other roles"))
		}
		request = qualitygate.BuilderReceiptRequest{Owner: *owner, AssignmentID: *id, TaskID: a.TaskID, CompletionRef: a.CompletionRef, RequiredChecks: a.RequiredChecks, DoneWhen: a.DoneWhen}
	}
	if request.AssignmentID == "" {
		return fail(fmt.Errorf("no matching registered owner/assignment"))
	}
	if err := fileview.ValidateAuthority(*root, snapshot.State); err != nil {
		return fail(err)
	}
	ref, err := fileview.DevelopmentRef(snapshot.State)
	if err != nil {
		return fail(err)
	}
	catalog, err := transition.LoadCatalog(*root)
	if err != nil {
		return fail(err)
	}
	view, err := fileview.New(*root, "refs/heads/"+strings.TrimPrefix(ref, "refs/heads/"), catalog.Definition.FileSources)
	if err != nil {
		return fail(err)
	}
	receipt := qualitygate.CurrentBuilderReceipt(qualitygate.Input{Root: *root, Snapshot: snapshot, Files: view}, request)
	data, err := json.Marshal(receipt)
	if err != nil {
		return fail(err)
	}
	if err := schema.NewEmbeddedValidator().ValidateBytes("builder-receipt.schema.json", data); err != nil {
		return fail(err)
	}
	if err := view.Verify(); err != nil {
		return fail(err)
	}
	currentData, err := json.Marshal(qualitygate.CurrentBuilderReceipt(qualitygate.Input{Root: *root, Snapshot: snapshot, Files: view}, request))
	if err != nil {
		return fail(err)
	}
	if !bytes.Equal(data, currentData) {
		return fail(fmt.Errorf("Builder receipt sources changed during observation: %w", runtime.ErrStaleRevision))
	}
	latest, err := store.Snapshot()
	if err != nil {
		return fail(err)
	}
	before, _ := json.Marshal(snapshot.State)
	after, _ := json.Marshal(latest.State)
	if latest.Revision != snapshot.Revision || !bytes.Equal(before, after) {
		return fail(runtime.ErrStaleRevision)
	}
	if err := view.Verify(); err != nil {
		return fail(err)
	}
	_, err = fmt.Fprintln(stdout, string(data))
	if err != nil {
		return fail(err)
	}
	return 0
}
