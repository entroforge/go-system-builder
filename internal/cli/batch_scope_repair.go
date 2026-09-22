package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/entroforge/go-system-builder/internal/docscope"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/semantic"
)

type batchScopePlan struct {
	Runtime       string            `json:"runtime_id"`
	Revision      int               `json:"revision"`
	StateHash     string            `json:"state_sha256"`
	Removed       []string          `json:"removed_document_ids"`
	RetainedTasks []string          `json:"retained_task_ids"`
	Invalidated   []string          `json:"invalidated_evidence_ids"`
	FileHashes    map[string]string `json:"file_hashes"`
	Target        string            `json:"target"`
}

func scopeHash(b []byte) string { s := sha256.Sum256(b); return hex.EncodeToString(s[:]) }
func scopeFile(root, rel string) ([]byte, error) {
	if filepath.IsAbs(rel) || filepath.ToSlash(filepath.Clean(rel)) != rel || strings.HasPrefix(rel, "../") {
		return nil, fmt.Errorf("unsafe document path %q", rel)
	}
	base, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, err
	}
	base, err = filepath.Abs(base)
	if err != nil {
		return nil, err
	}
	path, err := filepath.EvalSymlinks(filepath.Join(base, rel))
	if err != nil {
		return nil, err
	}
	if !strings.HasPrefix(path, base+string(os.PathSeparator)) {
		return nil, fmt.Errorf("document escapes root: %s", rel)
	}
	return os.ReadFile(path)
}
func inspectBatchScope(root string, state map[string]any) (batchScopePlan, error) {
	p := batchScopePlan{Runtime: stringValue(state["runtime_id"]), Revision: batchInt(state["revision"]), FileHashes: map[string]string{}, Target: "document_verification"}
	raw, _ := json.Marshal(state)
	p.StateHash = scopeHash(raw)
	life, _ := state["lifecycle"].(map[string]any)
	if life["state"] != "building" || state["pause"] != nil {
		return p, fmt.Errorf("repair requires unpaused building before Builder execution")
	}
	req, _ := state["bound_req"].(map[string]any)
	id := stringValue(req["id"])
	if id == "" || req["status"] != "locked" {
		return p, fmt.Errorf("locked binding required")
	}
	read := func(rel string, want any) ([]byte, error) {
		b, e := scopeFile(root, rel)
		if e != nil {
			return nil, e
		}
		h := scopeHash(b)
		if h != want {
			return nil, fmt.Errorf("fingerprint drift: %s", rel)
		}
		p.FileHashes[rel] = h
		return b, nil
	}
	if _, err := read(stringValue(req["path"]), req["sha256"]); err != nil {
		return p, err
	}
	review, _ := state["review"].(map[string]any)
	if batchInt(review["round"]) != 0 || review["clean_round"] != nil {
		return p, fmt.Errorf("review already started; manual recovery assessment required")
	}
	entities, _ := state["entities"].(map[string]any)
	agents, _ := entities["agents"].([]any)
	for _, r := range agents {
		a, _ := r.(map[string]any)
		if a["role"] != "document-verifier" {
			return p, fmt.Errorf("non-document agent exists; execution-aware recovery required")
		}
	}
	evs, _ := state["evidence"].([]any)
	for _, r := range evs {
		e, _ := r.(map[string]any)
		kind := stringValue(e["kind"])
		if strings.Contains(kind, "completion") || strings.Contains(kind, "builder") {
			return p, fmt.Errorf("Builder evidence exists; execution-aware recovery required")
		}
		if kind == "document_review" && e["status"] == "valid" {
			if _, err := read(stringValue(e["path"]), e["sha256"]); err != nil {
				return p, err
			}
			p.Invalidated = append(p.Invalidated, stringValue(e["id"]))
		}
	}
	// Integration artifacts imply execution even if completion registration failed.
	entries, err := os.ReadDir(filepath.Join(root, ".claude/evidence", p.Runtime, fmt.Sprintf("g%d", batchNestedInt(state, "baseline", "generation")), "worktree"))
	if err != nil && !os.IsNotExist(err) {
		return p, err
	}
	if len(entries) > 0 {
		return p, fmt.Errorf("integration workspace exists; execution-aware recovery required")
	}
	docs, _ := state["documents"].([]any)
	for _, r := range docs {
		d, _ := r.(map[string]any)
		if batchInt(d["generation"]) != batchNestedInt(state, "baseline", "generation") {
			continue
		}
		kind := stringValue(d["kind"])
		if kind != "task" && kind != "contract" && kind != "design" {
			continue
		}
		b, err := read(stringValue(d["path"]), d["sha256"])
		if err != nil {
			return p, err
		}
		if !docscope.Belongs(b, id) {
			p.Removed = append(p.Removed, stringValue(d["id"]))
		} else if kind == "task" {
			p.RetainedTasks = append(p.RetainedTasks, stringValue(d["id"]))
		}
	}
	if len(p.Removed) == 0 || len(p.RetainedTasks) == 0 {
		return p, fmt.Errorf("requires proven foreign documents and a nonempty retained task batch")
	}
	sort.Strings(p.Removed)
	sort.Strings(p.RetainedTasks)
	sort.Strings(p.Invalidated)
	return p, nil
}
func runBatchScopeRepair(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("runtime repair-batch-scope", flag.ContinueOnError)
	flags.SetOutput(stderr)
	root := flags.String("root", ".", "project root")
	planFile := flags.String("apply-plan", "", "apply exactly the JSON plan produced by the read-only invocation")
	if err := parseWorkspaceFlags(flags, args); err != nil {
		return 2
	}
	store := runtime.NewWriter(filepath.Join(*root, ".claude/loop-state.json"), filepath.Join(*root, ".claude/loop-events.jsonl"), *root, semantic.RuntimeCandidateValidator{})
	snapshot, err := store.Snapshot()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	plan, err := inspectBatchScope(*root, snapshot.State)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if *planFile == "" {
		return encodeJSON(stdout, plan)
	}
	b, err := os.ReadFile(*planFile)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	var approved batchScopePlan
	if json.Unmarshal(b, &approved) != nil || !reflect.DeepEqual(plan, approved) {
		fmt.Fprintln(stderr, "stale or changed repair plan; inspect again")
		return 1
	}
	event := fmt.Sprintf("evt-batch-scope-repaired-r%d", plan.Revision+1)
	_, err = store.Update(plan.Revision, runtime.Mutation{EventID: event, TransitionID: "BATCH-SCOPE-REPAIR", Actor: "framework-maintainer", Event: "batch_scope_repaired", IdempotencyKey: event, RuntimeID: plan.Runtime, From: map[string]any{"state": "building", "phase": nil}, To: map[string]any{"state": "document_verification", "phase": nil}, BaselineGeneration: batchNestedInt(snapshot.State, "baseline", "generation"), RequestID: "repair-batch-scope", OccurredAt: time.Now().UTC(), Message: fmt.Sprintf("Removed foreign registrations %v; retained tasks %v; invalidated document reviews %v; files preserved; repeat S5", plan.Removed, plan.RetainedTasks, plan.Invalidated), Apply: func(state map[string]any) error {
		current, err := inspectBatchScope(*root, state)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(current, plan) {
			return fmt.Errorf("repair facts changed")
		}
		remove := map[string]bool{}
		for _, id := range plan.Removed {
			remove[id] = true
		}
		docs, _ := state["documents"].([]any)
		kept := []any{}
		for _, r := range docs {
			d, _ := r.(map[string]any)
			if !remove[stringValue(d["id"])] || batchInt(d["generation"]) != batchNestedInt(state, "baseline", "generation") {
				kept = append(kept, r)
			}
		}
		state["documents"] = kept
		invalid := map[string]bool{}
		for _, id := range plan.Invalidated {
			invalid[id] = true
		}
		evs, _ := state["evidence"].([]any)
		for _, r := range evs {
			e, _ := r.(map[string]any)
			if invalid[stringValue(e["id"])] {
				e["status"] = "invalid"
				e["invalidated_by"] = "BATCH-SCOPE-REPAIR"
				e["invalidation_reason"] = "execution scope included foreign REQ documents; repeat S5"
				e["invalidation_rule"] = "batch_scope_repair"
			}
		}
		l := state["lifecycle"].(map[string]any)
		l["state"] = "document_verification"
		l["phase"] = nil
		l["phase_revision"] = batchInt(l["phase_revision"]) + 1
		delete(state, "milestone")
		return nil
	}})
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return encodeJSON(stdout, map[string]any{"status": "repaired", "plan": plan})
}

func batchInt(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case float64:
		return int(n)
	default:
		return 0
	}
}
func batchNestedInt(s map[string]any, key, field string) int {
	m, _ := s[key].(map[string]any)
	return batchInt(m[field])
}
