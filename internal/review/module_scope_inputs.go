package review

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/entroforge/go-system-builder/internal/fileview"
	"github.com/entroforge/go-system-builder/internal/schema"
)

// A preparation freezes a single development view outside the Runtime lock.
// CAS only rechecks the view identity, the authority projection and disk refs.
type preparedModuleScope struct {
	scope           ModuleScope
	changedPaths    []string
	view            *fileview.View
	authorityDigest string
	diskRefs        []ScopeInputRef
	root            string
}

func moduleScopeAuthorityDigest(state map[string]any) string {
	return scopeJSONDigest([]any{state["runtime_id"], state["bound_req"], state["baseline"], state["documents"], currentReviewRound(state), mapValue(state["review"])["round_entry"], scopeEvidenceCoordinates(state)})
}
func scopeEvidenceCoordinates(state map[string]any) []any {
	rows := []any{}
	for _, raw := range evidenceEntries(state) {
		e, _ := raw.(map[string]any)
		if e["kind"] == "completion_report" || e["kind"] == "change_impact" {
			rows = append(rows, []any{e["id"], e["kind"], e["path"], e["sha256"], e["baseline_generation"]})
		}
	}
	return rows
}
func (p *preparedModuleScope) Verify(state map[string]any) error {
	if p == nil {
		return fmt.Errorf("module scope preparation missing")
	}
	if p.authorityDigest != moduleScopeAuthorityDigest(state) {
		return fmt.Errorf("module scope authority moved; prepare again")
	}
	if err := p.view.Verify(); err != nil {
		return err
	}
	for _, ref := range p.diskRefs {
		data, err := fileview.Disk{Root: p.root}.ReadFile(ref.Path)
		if err != nil || sha256Of(data) != ref.SHA256 {
			return fmt.Errorf("module scope evidence drifted: %s", ref.Path)
		}
	}
	return nil
}
func lockedScopeDocument(state map[string]any, p, sha string) bool {
	for _, raw := range stateDocuments(state) {
		d, _ := raw.(map[string]any)
		if d["path"] == p && d["sha256"] == sha && d["status"] == "locked" && intField(d["generation"]) == baselineGeneration(state) {
			return true
		}
	}
	return false
}
func scopeGit(root string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "git", append([]string{"-C", root}, args...)...).Output()
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	return out, err
}
func scopeControlPath(p, workspace string) bool {
	// Only existing control/verification surfaces are outside the product diff.
	return p == ".claude/loop-state.json" || p == ".claude/loop-events.jsonl" || strings.HasPrefix(p, ".claude/evidence/") || strings.HasPrefix(p, ".claude/review/") || strings.HasPrefix(p, ".claude/workgroups/") || strings.HasPrefix(p, ".claude/assignments/") || strings.HasPrefix(p, ".claude/integration/") || (workspace != "" && (p == workspace || strings.HasPrefix(p, strings.TrimSuffix(workspace, "/")+"/")))
}

func prepareModuleScope(root string, state map[string]any, round int, workspace string) (*preparedModuleScope, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	view, err := fileview.ForState(root, state)
	if err != nil {
		return nil, fmt.Errorf("module scope source: %w", err)
	}
	view = view.WithContext(ctx)
	bound, _ := state["bound_req"].(map[string]any)
	binding, _ := bound["workspace"].(map[string]any)
	in := ScopeInputs{RuntimeID: stringField(state["runtime_id"]), REQID: stringField(bound["id"]), SourceCommit: view.Commit, BaselineGeneration: baselineGeneration(state), ReviewRound: round, Requirements: map[string][]ScenarioRef{}, UIImpact: boundREQUIImpact(state)}
	prepared := &preparedModuleScope{root: root, view: view.WithContext(context.Background()), authorityDigest: moduleScopeAuthorityDigest(state)}
	seen := map[string]bool{}
	read := func(files fileview.Reader, p, kind string, required, locked bool) []byte {
		if ctx.Err() != nil {
			in.UnresolvedInputs = append(in.UnresolvedInputs, "module scope preparation budget exhausted: "+ctx.Err().Error())
			return nil
		}
		p = strings.SplitN(p, "#", 2)[0]
		if !validScopePath(p, false) {
			in.UnresolvedInputs = append(in.UnresolvedInputs, "invalid input path: "+p)
			return nil
		}
		if kind == "git_tree" {
			source, err := view.Source(p)
			if err != nil || source != "git_tree" {
				in.UnresolvedInputs = append(in.UnresolvedInputs, "module scope specification/product input is not a Git-tree source: "+p)
				return nil
			}
		}
		data, e := files.ReadFile(p)
		ref := ScopeInputRef{Path: p, SourceKind: kind, Present: e == nil}
		if kind == "historical_tree" {
			if historical, ok := files.(*fileview.View); ok {
				ref.SourceCommit = historical.Commit
			}
		}
		if e == nil {
			ref.SHA256 = sha256Of(data)
		} else if required || !os.IsNotExist(e) {
			in.UnresolvedInputs = append(in.UnresolvedInputs, fmt.Sprintf("read %s input %s: %v", kind, p, e))
		}
		if !seen[kind+":"+ref.SourceCommit+":"+p] {
			in.InputRefs = append(in.InputRefs, ref)
			seen[kind+":"+ref.SourceCommit+":"+p] = true
			if (kind == "evidence" || kind == "prior_plan") && e == nil {
				prepared.diskRefs = append(prepared.diskRefs, ref)
			}
		}
		if locked && e == nil && !lockedScopeDocument(state, p, ref.SHA256) {
			in.UnresolvedInputs = append(in.UnresolvedInputs, "input not registered/locked at its current hash: "+p)
		}
		return data
	}
	reqPath := stringField(bound["path"])
	req := read(view, reqPath, "git_tree", true, false)
	if sha256Of(req) != stringField(bound["sha256"]) {
		in.UnresolvedInputs = append(in.UnresolvedInputs, "bound REQ fingerprint differs from fixed view")
	}
	seeds := func(data []byte) {
		for _, m := range e2eModuleRefPattern.FindAllStringSubmatch(string(data), -1) {
			in.SeedModules = append(in.SeedModules, m[1])
		}
	}
	seeds(req)
	parseGraph := func(data []byte, label string) *ModuleRelations {
		if data == nil {
			return nil
		}
		if err := schema.NewEmbeddedValidator().ValidateBytes("module-relations.schema.json", data); err != nil {
			in.UnresolvedInputs = append(in.UnresolvedInputs, label+": "+err.Error())
			return nil
		}
		var g ModuleRelations
		if e := json.Unmarshal(data, &g); e != nil {
			in.UnresolvedInputs = append(in.UnresolvedInputs, label+": "+e.Error())
			return nil
		}
		if e := ValidateModuleRelations(&g); e != nil {
			in.UnresolvedInputs = append(in.UnresolvedInputs, label+": "+e.Error())
			return nil
		}
		return &g
	}
	in.Graph = parseGraph(read(view, ModuleRelationsPath, "git_tree", false, true), "current relations")
	base := stringField(binding["bound_commit"])
	var previous *fileview.View
	if base == "" {
		in.UnresolvedInputs = append(in.UnresolvedInputs, "missing REQ baseline bound_commit")
	} else {
		previous, err = fileview.New(root, base, []fileview.Rule{{Path: ".", Source: "git_tree"}})
		if err != nil {
			in.UnresolvedInputs = append(in.UnresolvedInputs, "unverifiable REQ baseline: "+err.Error())
		} else {
			// Both endpoints of a rename are retained by --no-renames.
			out, e := scopeGit(root, "diff", "--no-renames", "--name-only", "-z", previous.Commit, view.Commit, "--")
			if e != nil {
				in.UnresolvedInputs = append(in.UnresolvedInputs, "cannot reconcile REQ Git diff: "+e.Error())
			} else {
				for _, p := range strings.Split(string(out), "\x00") {
					if p != "" && !scopeControlPath(p, workspace) {
						in.ChangedPaths = append(in.ChangedPaths, p)
					}
				}
			}
			in.PreviousGraph = parseGraph(read(previous, ModuleRelationsPath, "baseline_tree", false, false), "baseline relations")
		}
	}

	// A pinned S9 seed carries immutable prior-plan refs. Read the old fixed
	// graphs as well as both REQ endpoints: an edge added during S2 and later
	// removed in S9 must not erase its consumers from the new round.
	priorRefs := []ScopeInputRef{}
	if ptr := PlanPointerFromState(state); ptr != nil && ptr.ReviewRound < round {
		priorRefs = append(priorRefs, ScopeInputRef{Path: ptr.Path, SHA256: ptr.SHA256, Present: true, SourceKind: "prior_plan"})
	}
	entry := mapValue(mapValue(state["review"])["round_entry"])
	if intField(entry["round"]) == round && stringField(entry["transition_id"]) == "TR-012" {
		seedPath := stringField(entry["review_plan_seed_ref"])
		seedSHA := stringField(entry["review_plan_seed_sha256"])
		if seedPath != "" {
			seedData, e := fileview.Disk{Root: root}.ReadFile(seedPath)
			if e != nil || sha256Of(seedData) != seedSHA {
				in.UnresolvedInputs = append(in.UnresolvedInputs, "pinned S9 module-scope seed missing/drifted")
			} else {
				prepared.diskRefs = append(prepared.diskRefs, ScopeInputRef{Path: seedPath, SHA256: seedSHA, SourceKind: "evidence", Present: true})
				var seed Plan
				if json.Unmarshal(seedData, &seed) == nil && seed.ModuleScope != nil {
					for _, ref := range seed.ModuleScope.InputRefs {
						if ref.SourceKind == "prior_plan" {
							priorRefs = append(priorRefs, ref)
						}
					}
				} else {
					in.UnresolvedInputs = append(in.UnresolvedInputs, "S9 seed has no scoped prior-round provenance")
				}
			}
		}
	}
	priorSeen := map[string]bool{}
	for len(priorRefs) > 0 {
		ref := priorRefs[0]
		priorRefs = priorRefs[1:]
		if priorSeen[ref.Path] {
			continue
		}
		priorSeen[ref.Path] = true
		if _, e := repositoryContainedPath(root, ref.Path); e != nil {
			in.UnresolvedInputs = append(in.UnresolvedInputs, e.Error())
			continue
		}
		data := read(fileview.Disk{Root: root}, ref.Path, "prior_plan", true, false)
		if sha256Of(data) != ref.SHA256 {
			in.UnresolvedInputs = append(in.UnresolvedInputs, "prior ReviewPlan fingerprint mismatch: "+ref.Path)
			continue
		}
		var old Plan
		if e := schema.NewEmbeddedValidator().ValidateBytes("review-plan.schema.json", data); e != nil || json.Unmarshal(data, &old) != nil || old.ModuleScope == nil || old.ModuleScope.REQID != in.REQID || old.ModuleScope.RuntimeID != in.RuntimeID || old.ReviewRound >= round {
			in.UnresolvedInputs = append(in.UnresolvedInputs, "prior ReviewPlan has incompatible scope provenance: "+ref.Path)
			continue
		}
		historical, e := fileview.New(root, old.ModuleScope.SourceCommit, []fileview.Rule{{Path: ".", Source: "git_tree"}})
		if e != nil {
			in.UnresolvedInputs = append(in.UnresolvedInputs, "prior module graph tree unavailable: "+e.Error())
			continue
		}
		g := parseGraph(read(historical, ModuleRelationsPath, "historical_tree", false, false), "prior round relations")
		if g != nil {
			in.HistoricalGraphs = append(in.HistoricalGraphs, g)
		}
		for _, c := range old.ModuleScope.Candidates {
			in.SeedModules = append(in.SeedModules, c.ModuleID)
			in.KnownModules = append(in.KnownModules, c.ModuleID)
		}
		for _, oldRef := range old.ModuleScope.InputRefs {
			if oldRef.SourceKind == "prior_plan" {
				priorRefs = append(priorRefs, oldRef)
			}
		}
	}
	// Completion evidence is cumulative, including the original S6 generation
	// after an S9 baseline bump. A changed status never discards its denominator.
	registered := map[string]bool{}
	for _, raw := range evidenceEntries(state) {
		e, _ := raw.(map[string]any)
		if intField(e["baseline_generation"]) > in.BaselineGeneration {
			continue
		}
		kind := stringField(e["kind"])
		if kind != "completion_report" && kind != "change_impact" {
			continue
		}
		p := stringField(e["path"])
		if p == "" || stringField(e["sha256"]) == "" {
			in.UnresolvedInputs = append(in.UnresolvedInputs, "unfingerprinted scope evidence: "+stringField(e["id"]))
			continue
		}
		if _, er := repositoryContainedPath(root, p); er != nil {
			in.UnresolvedInputs = append(in.UnresolvedInputs, er.Error())
			continue
		}
		data := read(fileview.Disk{Root: root}, p, "evidence", true, false)
		if sha256Of(data) != stringField(e["sha256"]) {
			in.UnresolvedInputs = append(in.UnresolvedInputs, "scope evidence hash mismatch: "+p)
			continue
		}
		if kind == "completion_report" {
			paths, er := completionChangedPaths(root, e)
			if er != nil {
				in.UnresolvedInputs = append(in.UnresolvedInputs, er.Error())
			}
			for _, p := range paths {
				registered[p] = true
				in.ChangedPaths = append(in.ChangedPaths, p)
			}
		} else {
			var impact repairChangeImpactArtifact
			if er := json.Unmarshal(data, &impact); er != nil || impact.RuntimeID != in.RuntimeID {
				in.UnresolvedInputs = append(in.UnresolvedInputs, "invalid repair impact: "+p)
				continue
			}
			for _, a := range append(impact.ChangedArtifacts, impact.VerifiedSubjects...) {
				registered[a.Path] = true
				in.ChangedPaths = append(in.ChangedPaths, a.Path)
			}
		}
	}
	for _, p := range sortedDistinct(in.ChangedPaths) {
		dataAtPath, readErr := view.ReadFile(p)
		locked := readErr == nil && lockedScopeDocument(state, p, sha256Of(dataAtPath))
		if locked {
			for _, raw := range stateDocuments(state) {
				d := mapValue(raw)
				if d["path"] == p {
					switch d["kind"] {
					case "design", "contract", "task", "dispatch_plan":
						in.LockedDocumentPaths = append(in.LockedDocumentPaths, p)
					}
				}
			}
		}
		if !registered[p] && !locked {
			in.UnresolvedInputs = append(in.UnresolvedInputs, "Git change absent from registered S6/S9 evidence: "+p)
		}
		read(view, p, "git_tree", false, false)
		if previous != nil {
			read(previous, p, "baseline_tree", false, false)
		}
	}
	// Inventory membership participates in the digest, so a new module/CASE
	// cannot silently remain outside previously known input refs.
	entries, e := view.ReadDir("docs/design/prototypes")
	if e != nil && !os.IsNotExist(e) {
		in.UnresolvedInputs = append(in.UnresolvedInputs, "prototype discovery failed: "+e.Error())
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		id := entry.Name()
		if !moduleIDPattern.MatchString(id) {
			in.UnresolvedInputs = append(in.UnresolvedInputs, "unrecognized prototype module: "+id)
			continue
		}
		in.KnownModules = append(in.KnownModules, id)
		in.DiscoveryManifest = append(in.DiscoveryManifest, "module:"+id)
	}
	modules := map[string]ModuleDeclaration{}
	for _, g := range append(append([]*ModuleRelations{}, in.HistoricalGraphs...), in.PreviousGraph, in.Graph) {
		if g == nil {
			continue
		}
		for _, m := range g.Modules {
			modules[m.ID] = m
			in.KnownModules = append(in.KnownModules, m.ID)
		}
	}
	for _, id := range sortedDistinct(in.KnownModules) {
		m, declared := modules[id]
		if !declared {
			m = ModuleDeclaration{ID: id, CasesPath: "docs/design/prototypes/" + id + "/cases.json"}
			if in.Graph != nil {
				in.UnresolvedInputs = append(in.UnresolvedInputs, "prototype module missing from relations: "+id)
			}
		}
		in.DiscoveryManifest = append(in.DiscoveryManifest, "module:"+id+":"+m.CasesPath)
		for _, ref := range append(append([]string{}, m.ContractRefs...), m.ApplicabilityRefs...) {
			data := read(view, ref, "git_tree", true, true)
			seeds(data)
		}
		if m.CasesPath == "" {
			continue
		}
		data := read(view, m.CasesPath, "git_tree", true, true)
		// A deleted module retains its old CASE obligations.
		if data == nil && previous != nil {
			data = read(previous, m.CasesPath, "baseline_tree", true, false)
		}
		var doc e2eCasesDocument
		if er := json.Unmarshal(data, &doc); er != nil || doc.Module != id {
			in.UnresolvedInputs = append(in.UnresolvedInputs, "CASE inventory invalid/module mismatch: "+id)
			continue
		}
		caseIDs := map[string]bool{}
		for _, c := range doc.Cases {
			if c.ID == "" || caseIDs[c.ID] {
				in.UnresolvedInputs = append(in.UnresolvedInputs, "duplicate/empty CASE identity in "+m.CasesPath)
				continue
			}
			caseIDs[c.ID] = true
			in.DiscoveryManifest = append(in.DiscoveryManifest, m.CasesPath+":"+c.ID)
			if !c.Required || !c.BrowserRequired {
				continue
			}
			paths := []string{}
			for _, p := range c.FlowRefs {
				if strings.HasPrefix(p, "PATH-") {
					paths = append(paths, p)
				}
			}
			if len(paths) == 0 {
				in.UnresolvedInputs = append(in.UnresolvedInputs, "required browser CASE has no PATH: "+m.CasesPath+"#"+c.ID)
			}
			in.Requirements[id] = append(in.Requirements[id], ScenarioRef{CasesPath: m.CasesPath, CaseID: c.ID, PathIDs: sortedDistinct(paths)})
		}
		sort.Slice(in.Requirements[id], func(i, j int) bool { return in.Requirements[id][i].CaseID < in.Requirements[id][j].CaseID })
		// Flow/oracle documents in the module directory belong to the same view.
		dir := strings.TrimSuffix(m.CasesPath, "/cases.json")
		members, er := view.ReadDir(dir)
		if er != nil {
			in.UnresolvedInputs = append(in.UnresolvedInputs, "cannot enumerate CASE/flow directory: "+dir)
		}
		for _, member := range members {
			if member.IsDir() {
				in.UnresolvedInputs = append(in.UnresolvedInputs, "nested CASE/flow directory needs explicit module ownership: "+dir+"/"+member.Name())
				continue
			}
			p := dir + "/" + member.Name()
			in.DiscoveryManifest = append(in.DiscoveryManifest, p)
			read(view, p, "git_tree", true, strings.HasSuffix(p, ".json") || strings.HasSuffix(p, ".md") || strings.HasSuffix(p, ".html"))
		}
	}
	if in.Graph != nil {
		for _, r := range in.Graph.Resources {
			for _, ref := range r.SourceRefs {
				data := read(view, ref, "git_tree", true, true)
				seeds(data)
			}
		}
	}
	in.SeedModules = sortedDistinct(in.SeedModules)
	out, err := ResolveModuleScope(in)
	if err != nil {
		return nil, err
	}
	prepared.scope = out
	prepared.changedPaths = sortedDistinct(in.ChangedPaths)
	if err = view.Verify(); err != nil {
		return nil, err
	}
	return prepared, nil
}

// ResolveModuleScopeForRoot is the production reader. Author-only callers
// must never substitute disk data for this Runtime-bound fixed-view contract.
func ResolveModuleScopeForRoot(root string, state map[string]any, round int, workspace string) (ModuleScope, error) {
	p, e := prepareModuleScope(root, state, round, workspace)
	if e != nil {
		return ModuleScope{}, e
	}
	return p.scope, nil
}
