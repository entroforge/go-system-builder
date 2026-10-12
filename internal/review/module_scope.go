package review

import (
	"encoding/json"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"
)

const ModuleRelationsPath = "docs/architecture/module-relations.json"
const ModuleScopeResolverVersion = "1.0.0"

type ModuleRelations struct {
	SchemaVersion string              `json:"schema_version"`
	Modules       []ModuleDeclaration `json:"modules"`
	Resources     []ModuleResource    `json:"resources"`
}
type ModuleDeclaration struct {
	ID                   string   `json:"id"`
	UIPaths              []string `json:"ui_paths,omitempty"`
	OwnedPaths           []string `json:"owned_paths"`
	CasesPath            string   `json:"cases_path,omitempty"`
	ContractRefs         []string `json:"contract_refs"`
	BrowserApplicability string   `json:"browser_applicability"`
	ApplicabilityRefs    []string `json:"applicability_refs,omitempty"`
}
type ModuleResource struct {
	ID                string   `json:"id"`
	Paths             []string `json:"paths"`
	ProviderModuleIDs []string `json:"provider_module_ids"`
	ConsumerModuleIDs []string `json:"consumer_module_ids"`
	SourceRefs        []string `json:"source_refs"`
}
type ScopeInputRef struct {
	SourceCommit string `json:"source_commit,omitempty"`
	Path         string `json:"path"`
	SHA256       string `json:"sha256"`
	SourceKind   string `json:"source_kind"`
	Present      bool   `json:"present"`
}
type ScenarioRef struct {
	CasesPath string   `json:"cases_path"`
	CaseID    string   `json:"case_id"`
	PathIDs   []string `json:"path_ids"`
}
type ModuleCandidate struct {
	ModuleID        string        `json:"module_id"`
	Reasons         []string      `json:"reasons"`
	RequirementRefs []ScenarioRef `json:"requirement_refs"`
	BrowserRequired bool          `json:"browser_required"`
	DirectUI        bool          `json:"direct_ui"`
}
type ModuleDisposition struct {
	ModuleID             string          `json:"module_id"`
	Action               string          `json:"action"`
	Rationale            string          `json:"rationale"`
	EvidenceRefs         []ScopeInputRef `json:"evidence_refs"`
	VerificationClaimIDs []string        `json:"verification_claim_ids"`
	AssessmentClaimIDs   []string        `json:"assessment_claim_ids"`
}
type ModuleScope struct {
	ResolverVersion         string              `json:"resolver_version"`
	RuntimeID               string              `json:"runtime_id"`
	REQID                   string              `json:"req_id"`
	BaselineGeneration      int                 `json:"baseline_generation"`
	ReviewRound             int                 `json:"review_round"`
	SourceCommit            string              `json:"source_commit"`
	InputRefs               []ScopeInputRef     `json:"input_refs"`
	DiscoveryManifestDigest string              `json:"discovery_manifest_digest"`
	InputDigest             string              `json:"input_digest"`
	Resolution              string              `json:"resolution"`
	UnresolvedInputs        []string            `json:"unresolved_inputs"`
	Candidates              []ModuleCandidate   `json:"candidates"`
	Dispositions            []ModuleDisposition `json:"dispositions"`
}

// ScopeInputs is a fixed, verified snapshot; the resolver performs no I/O.
type ScopeInputs struct {
	RuntimeID, REQID, SourceCommit                            string
	BaselineGeneration, ReviewRound                           int
	HistoricalGraphs                                          []*ModuleRelations
	Graph, PreviousGraph                                      *ModuleRelations
	ChangedPaths, SeedModules, KnownModules, UnresolvedInputs []string
	LockedDocumentPaths                                       []string
	InputRefs                                                 []ScopeInputRef
	Requirements                                              map[string][]ScenarioRef
	DiscoveryManifest                                         []string
	UIImpact                                                  string
}

var moduleIDPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

func validScopePath(p string, directory bool) bool {
	if p == "" || strings.ContainsAny(p, "\\:\x00") || strings.HasPrefix(p, "/") {
		return false
	}
	q := strings.TrimSuffix(p, "/")
	return q != "." && q != ".." && !strings.HasPrefix(q, "../") && path.Clean(q) == q && (directory || !strings.HasSuffix(p, "/"))
}
func scopePathMatches(pattern, p string) bool {
	if strings.HasSuffix(pattern, "/") {
		return strings.HasPrefix(p, pattern)
	}
	return pattern == p
}
func ValidateModuleRelations(g *ModuleRelations) error {
	if g == nil || g.SchemaVersion != "1.0.0" || len(g.Modules) == 0 {
		return fmt.Errorf("module relations require version 1.0.0 and a nonempty module inventory")
	}
	ids := map[string]bool{}
	for _, m := range g.Modules {
		if !moduleIDPattern.MatchString(m.ID) || ids[m.ID] {
			return fmt.Errorf("invalid/duplicate module id %q", m.ID)
		}
		ids[m.ID] = true
		if len(m.OwnedPaths) == 0 {
			return fmt.Errorf("module %s has no ownership", m.ID)
		}
		for _, p := range m.OwnedPaths {
			if !validScopePath(p, true) {
				return fmt.Errorf("invalid ownership path %q", p)
			}
		}
		for _, p := range m.UIPaths {
			if !validScopePath(p, true) {
				return fmt.Errorf("invalid UI path %q", p)
			}
		}
		if len(m.UIPaths) > 0 && m.BrowserApplicability != "required" {
			return fmt.Errorf("module %s declares UI paths but no browser applicability", m.ID)
		}
		if m.CasesPath != "" && (!validScopePath(m.CasesPath, false) || path.Base(m.CasesPath) != "cases.json") {
			return fmt.Errorf("invalid cases path %q", m.CasesPath)
		}
		if m.BrowserApplicability != "required" && m.BrowserApplicability != "not_applicable" && m.BrowserApplicability != "unknown" {
			return fmt.Errorf("module %s: invalid browser applicability", m.ID)
		}
		if m.BrowserApplicability != "required" && len(m.ApplicabilityRefs) == 0 {
			return fmt.Errorf("module %s: applicability needs source references", m.ID)
		}
		for _, ref := range append(append([]string{}, m.ContractRefs...), m.ApplicabilityRefs...) {
			if !validScopePath(strings.SplitN(ref, "#", 2)[0], false) {
				return fmt.Errorf("invalid module source reference %q", ref)
			}
		}
	}
	resources := map[string]bool{}
	for _, r := range g.Resources {
		if !moduleIDPattern.MatchString(r.ID) || resources[r.ID] || len(r.Paths) == 0 || len(r.ProviderModuleIDs) == 0 || len(r.ConsumerModuleIDs) == 0 || len(r.SourceRefs) == 0 {
			return fmt.Errorf("invalid/duplicate/incomplete resource %q", r.ID)
		}
		resources[r.ID] = true
		for _, p := range r.Paths {
			if !validScopePath(p, true) {
				return fmt.Errorf("invalid resource path %q", p)
			}
		}
		for _, id := range append(append([]string{}, r.ProviderModuleIDs...), r.ConsumerModuleIDs...) {
			if !ids[id] {
				return fmt.Errorf("resource %s references unknown module %s", r.ID, id)
			}
		}
		for _, ref := range r.SourceRefs {
			if !validScopePath(strings.SplitN(ref, "#", 2)[0], false) {
				return fmt.Errorf("invalid resource source %q", ref)
			}
		}
	}
	return nil
}
func scopeJSONDigest(v any) string { b, _ := json.Marshal(v); return sha256Of(b) }

func ResolveModuleScope(in ScopeInputs) (ModuleScope, error) {
	out := ModuleScope{ResolverVersion: ModuleScopeResolverVersion, RuntimeID: in.RuntimeID, REQID: in.REQID, BaselineGeneration: in.BaselineGeneration, ReviewRound: in.ReviewRound, SourceCommit: in.SourceCommit, Resolution: "exact", InputRefs: append([]ScopeInputRef{}, in.InputRefs...), UnresolvedInputs: append([]string{}, in.UnresolvedInputs...), Candidates: []ModuleCandidate{}, Dispositions: []ModuleDisposition{}}
	graphs := []*ModuleRelations{}
	modules := map[string]ModuleDeclaration{}
	for _, g := range append(append([]*ModuleRelations{}, in.HistoricalGraphs...), in.PreviousGraph, in.Graph) {
		if g != nil {
			if err := ValidateModuleRelations(g); err != nil {
				return out, err
			}
			graphs = append(graphs, g)
			for _, m := range g.Modules {
				modules[m.ID] = m
			}
		}
	}
	reasons := map[string]map[string]bool{}
	directUI := map[string]bool{}
	add := func(id, reason string) {
		if reasons[id] == nil {
			reasons[id] = map[string]bool{}
		}
		reasons[id][reason] = true
	}
	for _, id := range in.SeedModules {
		if _, ok := modules[id]; ok {
			add(id, "direct:req-or-contract:"+id)
		} else if len(graphs) > 0 {
			out.UnresolvedInputs = append(out.UnresolvedInputs, "referenced module is undeclared: "+id)
		}
	}
	for _, p := range sortedDistinct(in.ChangedPaths) {
		owned := false
		for _, g := range graphs {
			for _, m := range g.Modules {
				for _, pattern := range m.OwnedPaths {
					if scopePathMatches(pattern, p) {
						owned = true
						add(m.ID, "direct:changed-path:"+p)
						if strings.HasPrefix(p, "web/") || strings.HasPrefix(p, "frontend/") || strings.HasPrefix(p, "client/") || strings.HasPrefix(p, "public/") || (m.BrowserApplicability == "required" && in.UIImpact != "none" && in.UIImpact != "") || strings.HasSuffix(p, ".tsx") || strings.HasSuffix(p, ".vue") || strings.HasSuffix(p, ".html") {
							directUI[m.ID] = true
						}
					}
				}
				for _, uiPath := range m.UIPaths {
					if scopePathMatches(uiPath, p) {
						owned = true
						directUI[m.ID] = true
						add(m.ID, "direct:ui-path:"+p)
					}
				}
				if m.CasesPath != "" && (p == m.CasesPath || strings.HasPrefix(p, path.Dir(m.CasesPath)+"/")) {
					owned = true
					directUI[m.ID] = true
					add(m.ID, "direct:cases:"+p)
				}
				for _, ref := range append(append([]string{}, m.ContractRefs...), m.ApplicabilityRefs...) {
					if p == strings.SplitN(ref, "#", 2)[0] {
						owned = true
						add(m.ID, "direct:contract:"+p)
					}
				}
			}
			for _, r := range g.Resources {
				for _, pattern := range r.Paths {
					if scopePathMatches(pattern, p) {
						owned = true
						for _, id := range append(append([]string{}, r.ProviderModuleIDs...), r.ConsumerModuleIDs...) {
							add(id, "direct:resource:"+r.ID+":"+p)
						}
					}
				}
				for _, ref := range r.SourceRefs {
					if p == strings.SplitN(ref, "#", 2)[0] {
						owned = true
						for _, id := range append(append([]string{}, r.ProviderModuleIDs...), r.ConsumerModuleIDs...) {
							add(id, "direct:resource-source:"+ref)
						}
					}
				}
			}
		}
		if p == ModuleRelationsPath {
			owned = true
			for id := range modules {
				add(id, "direct:relation-change")
			}
		}
		if !owned && containsString(in.LockedDocumentPaths, p) {
			// A hash-locked architecture/TASK/dispatch document is a real
			// specification input, not an unknown product file. Without more
			// precise ownership, assess every known module rather than making
			// the ordinary S2-S5 document path impossible to complete.
			owned = true
			out.Resolution = "conservative_fallback"
			for _, id := range in.KnownModules {
				add(id, "conservative_fallback:locked-specification-change:"+p)
			}
			for id := range modules {
				add(id, "conservative_fallback:locked-specification-change:"+p)
			}
		}
		if !owned {
			out.UnresolvedInputs = append(out.UnresolvedInputs, "changed path has no module/resource ownership: "+p)
		}
	}
	for changed := true; changed; {
		changed = false
		for _, g := range graphs {
			for _, r := range g.Resources {
				for _, provider := range r.ProviderModuleIDs {
					if reasons[provider] == nil {
						continue
					}
					for _, consumer := range r.ConsumerModuleIDs {
						if reasons[consumer] == nil {
							changed = true
						}
						add(consumer, "dependent:"+provider+"->"+r.ID+"->"+consumer)
					}
				}
			}
		}
	}
	if in.Graph == nil || in.PreviousGraph == nil {
		out.Resolution = "conservative_fallback"
		for _, id := range in.KnownModules {
			add(id, "conservative_fallback:incomplete-relations-or-baseline")
		}
		for id := range modules {
			add(id, "conservative_fallback:incomplete-relations-or-baseline")
		}
	}
	if len(reasons) == 0 {
		out.UnresolvedInputs = append(out.UnresolvedInputs, "cannot establish a nonempty complete module inventory")
	}
	for _, id := range sortedKeysOfReasons(reasons) {
		m := modules[id]
		refs := append([]ScenarioRef{}, in.Requirements[id]...)
		browser := m.BrowserApplicability == "required" || len(refs) > 0
		if m.BrowserApplicability == "unknown" {
			out.UnresolvedInputs = append(out.UnresolvedInputs, "browser applicability unknown: "+id)
		}
		if browser && len(refs) == 0 {
			out.UnresolvedInputs = append(out.UnresolvedInputs, "required browser module lacks required CASE/PATH inventory: "+id)
		}
		out.Candidates = append(out.Candidates, ModuleCandidate{ModuleID: id, Reasons: sortedKeys(reasons[id]), RequirementRefs: refs, BrowserRequired: browser, DirectUI: directUI[id] || (in.UIImpact == "changed" && len(in.SeedModules) > 0 && containsString(in.SeedModules, id))})
	}
	out.UnresolvedInputs = sortedDistinct(out.UnresolvedInputs)
	sort.Slice(out.InputRefs, func(i, j int) bool {
		a, b := out.InputRefs[i], out.InputRefs[j]
		return a.SourceKind+":"+a.SourceCommit+":"+a.Path < b.SourceKind+":"+b.SourceCommit+":"+b.Path
	})
	out.DiscoveryManifestDigest = scopeJSONDigest(sortedDistinct(in.DiscoveryManifest))
	// Commit identity is descriptive, not a coarse invalidation key. Only dependent content matters.
	out.InputDigest = scopeJSONDigest([]any{out.ResolverVersion, out.RuntimeID, out.REQID, out.BaselineGeneration, out.ReviewRound, out.InputRefs, out.DiscoveryManifestDigest, sortedDistinct(in.ChangedPaths), out.Candidates, out.Resolution, out.UnresolvedInputs})
	return out, nil
}
func sortedKeysOfReasons(m map[string]map[string]bool) []string {
	keys := []string{}
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
