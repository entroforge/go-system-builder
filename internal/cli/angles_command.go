// Package cli provides loop-harness command-line entry points. This file
// implements the `angles` subcommand family that REQ-003 defines as the
// review team's single interaction surface with the module-level angles
// registry (REQ-003 FR-005..FR-008, TASK-003-B).
//
// Subcommands:
//
//	loop-harness angles list    --baseline-for <REQ-nnn>    (FR-006 aggregate by Change Record scope.include)
//	loop-harness angles commit  --module <path> ...         (FR-005 create)
//	loop-harness angles retract --module <path> --id <id> ...
//	loop-harness angles revive  --module <path> --id <id> ...
//	loop-harness angles audit   --module <path>... --current-req <REQ-nnn> --scope-include <path>...
//
// All registry mutation paths funnel through the runtime package
// (`internal/runtime/angles.go`); the CLI is a thin argument parser that
// produces JSON. Path mapping is delegated to
// `runtime.AngleRegistryFileName` so the CLI stays in sync with the
// schema (FR-005 closing contract).
package cli

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/entroforge/go-system-builder/internal/runtime"
)

// angleIDPatternRE mirrors runtime.angleIDPattern but is duplicated here so
// the CLI can validate user input before the runtime API is invoked.
// Kept in lock-step with internal/runtime/angles.go.
var angleIDPatternRE = regexp.MustCompile(`^ANG-[A-Z0-9]+-[0-9]{3,}$`)

// reqIDPatternRE mirrors the REQ id shape used across the harness.
var reqIDPatternRE = regexp.MustCompile(`^REQ-([0-9]{3,})$`)

// runAngles is the dispatcher for the `loop-harness angles` subcommand family.
// It is called from Run() once args[0] == "angles". args is the slice AFTER
// "angles" (so args[0] is the subcommand).
func runAngles(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "angles requires <list|commit|retract|revive|audit>")
		return 2
	}
	switch args[0] {
	case "list":
		return runAnglesList(args[1:], stdout, stderr)
	case "commit":
		return runAnglesCommit(args[1:], stdout, stderr)
	case "retract":
		return runAnglesRetract(args[1:], stdout, stderr)
	case "revive":
		return runAnglesRevive(args[1:], stdout, stderr)
	case "audit":
		return runAnglesAudit(args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "unknown angles subcommand %q\n", args[0])
		return 2
	}
}

// stringsFlag is a repeatable string flag for path lists. REQ-003 FR-006
// requires aggregating across multiple scope.include paths, so the
// `list` and `audit` subcommands accept repeatable --module /
// --scope-include flags.
type stringsFlag []string

func (s *stringsFlag) String() string     { return strings.Join(*s, ",") }
func (s *stringsFlag) Set(v string) error { *s = append(*s, v); return nil }

// runAnglesList aggregates active angles across all modules in the active
// Change Record's scope.include (REQ-003 FR-006). Output is JSON shaped to
// match the team-manifest `inherited_angles` array.
func runAnglesList(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("angles list", flag.ContinueOnError)
	flags.SetOutput(stderr)
	bindUsage(flags, "angles list")
	root := flags.String("root", ".", "repository root")
	statePath := flags.String("state", ".claude/loop-state.json", "runtime state path")
	baselineFor := flags.String("baseline-for", "", "REQ id whose active Change Record drives scope.include aggregation")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if *baselineFor == "" {
		fmt.Fprintln(stderr, "angles list requires --baseline-for")
		return 2
	}
	if !reqIDPatternRE.MatchString(*baselineFor) {
		fmt.Fprintln(stderr, "angles list: --baseline-for must match REQ-NNN")
		return 2
	}
	scope, err := loadScopeInclude(*root, *statePath, *baselineFor)
	if err != nil {
		fmt.Fprintln(stderr, formatFailure("angles list", err))
		return 1
	}
	baseline, err := runtime.ListBaselineFor(*root, scope)
	if err != nil {
		fmt.Fprintln(stderr, formatFailure("angles list", err))
		return 1
	}
	// Sort by (module, id) for deterministic output (the docstring on
	// ListBaselineFor promises "sorted by (module, id)").
	sort.Slice(baseline, func(i, j int) bool {
		if baseline[i].Module != baseline[j].Module {
			return baseline[i].Module < baseline[j].Module
		}
		return baseline[i].ID < baseline[j].ID
	})
	out := map[string]any{
		"baseline_for":          *baselineFor,
		"scope_include":         scope,
		"modules_with_registry": countModulesWithRegistry(*root, scope),
		"inherited_angles":      baseline,
		"count":                 len(baseline),
	}
	return encodeJSON(stdout, out)
}

// countModulesWithRegistry returns how many of the input modules actually
// have a registry file (vs. no angles declared yet). Backs the CLI's
// observability of FR-006: missing registries are normal during bootstrap
// (REQ-003 Q-009) but should be visible in the baseline output.
func countModulesWithRegistry(root string, modules []string) int {
	n := 0
	for _, m := range modules {
		if _, err := os.Stat(runtime.AngleRegistryFilePath(root, m)); err == nil {
			n++
		}
	}
	return n
}

// loadScopeInclude reads the runtime state file and returns the
// scope.include path list from the active Change Record for the given REQ.
// If no Change Record is present, the caller is operating in a
// pre-bind / bootstrap state (REQ-003 Q-009) and we return an empty list
// rather than failing — `angles list --baseline-for` must work even when no
// Change Record exists yet.
func loadScopeInclude(root, statePath, reqID string) ([]string, error) {
	data, err := os.ReadFile(filepath.Join(root, statePath))
	if err != nil {
		// Missing state file is a normal bootstrap state; surface empty scope.
		if errors.Is(err, os.ErrNotExist) {
			return []string{}, nil
		}
		return nil, fmt.Errorf("read runtime state: %w", err)
	}
	state := map[string]any{}
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("decode runtime state: %w", err)
	}
	rawChange, ok := state["change"].(map[string]any)
	if !ok || rawChange == nil {
		// No active Change Record; bootstrap-empty baseline.
		return []string{}, nil
	}
	reqRef, _ := rawChange["req_ref"].(string)
	if reqRef != "" && reqRef != reqID {
		// Change Record is bound to a different REQ — surface as empty so
		// the caller does not silently aggregate the wrong scope.
		return []string{}, nil
	}
	scope, _ := rawChange["scope"].(map[string]any)
	rawInclude, _ := scope["include"].([]any)
	out := make([]string, 0, len(rawInclude))
	for _, v := range rawInclude {
		if s, ok := v.(string); ok && s != "" {
			out = append(out, s)
		}
	}
	return out, nil
}

// runAnglesCommit creates a new active angle in the given module. The ID is
// assigned by the runtime API; only statement / target / declared_in are
// user-supplied (REQ-003 FR-005). The module path is normalized (trimmed)
// so that the persisted `module` field matches the registry file name
// produced by `runtime.AngleRegistryFileName`; this guarantees the CLI
// and schema share a single canonical form (FR-005 closing contract).
func runAnglesCommit(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("angles commit", flag.ContinueOnError)
	flags.SetOutput(stderr)
	bindUsage(flags, "angles commit")
	root := flags.String("root", ".", "repository root")
	module := flags.String("module", "", "module path (e.g. internal/change)")
	statement := flags.String("statement", "", "angle statement (single sentence)")
	target := flags.String("target", "", "concrete examination target (file/invariant/failure-mode)")
	declaredIn := flags.String("declared-in", "", "REQ id (e.g. REQ-003) declaring this angle")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	for _, check := range []struct{ name, val string }{
		{"--module", *module},
		{"--statement", *statement},
		{"--target", *target},
		{"--declared-in", *declaredIn},
	} {
		if strings.TrimSpace(check.val) == "" {
			fmt.Fprintf(stderr, "angles commit: %s is required\n", check.name)
			return 2
		}
	}
	if !reqIDPatternRE.MatchString(*declaredIn) {
		fmt.Fprintln(stderr, "angles commit: --declared-in must match REQ-NNN")
		return 2
	}
	normalizedModule := normalizeModule(*module)
	reg, angle, err := runtime.CreateAngle(*root, runtime.CreateAngleRequest{
		ModulePath: normalizedModule,
		Statement:  *statement,
		Target:     *target,
		DeclaredIn: *declaredIn,
	})
	if err != nil {
		fmt.Fprintln(stderr, formatFailure("angles commit", err))
		return 1
	}
	return encodeJSON(stdout, map[string]any{
		"module":           reg.Module,
		"registry_version": reg.Version,
		"angle":            angle,
	})
}

// runAnglesRetract flags an active angle as retracted. Reason is mandatory
// (REQ-003 FR-007 / C-007 append-mostly); empty / whitespace reasons are
// rejected at the CLI boundary so we never let the runtime lose the
// required reason check via a silent argument drop.
func runAnglesRetract(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("angles retract", flag.ContinueOnError)
	flags.SetOutput(stderr)
	bindUsage(flags, "angles retract")
	root := flags.String("root", ".", "repository root")
	module := flags.String("module", "", "module path")
	id := flags.String("id", "", "angle id (ANG-{MODULE}-{NNN})")
	reqID := flags.String("req", "", "REQ id performing the retract")
	reason := flags.String("reason", "", "retract reason (mandatory; FR-007 / C-007)")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	for _, check := range []struct{ name, val string }{
		{"--module", *module},
		{"--id", *id},
		{"--req", *reqID},
		{"--reason", *reason},
	} {
		if strings.TrimSpace(check.val) == "" {
			fmt.Fprintf(stderr, "angles retract: %s is required\n", check.name)
			return 2
		}
	}
	if !angleIDPatternRE.MatchString(*id) {
		fmt.Fprintln(stderr, "angles retract: --id must match ANG-{MODULE}-{NNN}")
		return 2
	}
	if !reqIDPatternRE.MatchString(*reqID) {
		fmt.Fprintln(stderr, "angles retract: --req must match REQ-NNN")
		return 2
	}
	if strings.TrimSpace(*reason) == "" {
		fmt.Fprintln(stderr, "angles retract: --reason is required (FR-007 append-mostly; empty reason would lose audit trail)")
		return 2
	}
	reg, err := runtime.RetractAngle(*root, *module, *id, *reqID, *reason)
	if err != nil {
		fmt.Fprintln(stderr, formatFailure("angles retract", err))
		return 1
	}
	return encodeJSON(stdout, map[string]any{
		"module":           reg.Module,
		"registry_version": reg.Version,
		"angle_id":         *id,
		"status":           runtime.AngleStatusRetracted,
	})
}

// runAnglesRevive flips a retracted angle back to active. The runtime API
// already enforces single-revive (FR-007 / C-007), but the CLI surfaces a
// clear error before the call so invocation-trace logs read cleanly when
// an agent retries after a previous revive.
func runAnglesRevive(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("angles revive", flag.ContinueOnError)
	flags.SetOutput(stderr)
	bindUsage(flags, "angles revive")
	root := flags.String("root", ".", "repository root")
	module := flags.String("module", "", "module path")
	id := flags.String("id", "", "angle id (ANG-{MODULE}-{NNN})")
	reqID := flags.String("req", "", "REQ id performing the revive")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	for _, check := range []struct{ name, val string }{
		{"--module", *module},
		{"--id", *id},
		{"--req", *reqID},
	} {
		if strings.TrimSpace(check.val) == "" {
			fmt.Fprintf(stderr, "angles revive: %s is required\n", check.name)
			return 2
		}
	}
	if !angleIDPatternRE.MatchString(*id) {
		fmt.Fprintln(stderr, "angles revive: --id must match ANG-{MODULE}-{NNN}")
		return 2
	}
	if !reqIDPatternRE.MatchString(*reqID) {
		fmt.Fprintln(stderr, "angles revive: --req must match REQ-NNN")
		return 2
	}
	reg, err := runtime.ReviveAngle(*root, *module, *id, *reqID)
	if err != nil {
		fmt.Fprintln(stderr, formatFailure("angles revive", err))
		return 1
	}
	return encodeJSON(stdout, map[string]any{
		"module":           reg.Module,
		"registry_version": reg.Version,
		"angle_id":         *id,
		"status":           runtime.AngleStatusActive,
	})
}

// runAnglesAudit scans modules for active angles that should be flagged
// stale under FR-008 ("连续 3 轮未 disposition 自动标 stale"). Only modules
// OUTSIDE --scope-include have stale-eligible angles; scope-include angles
// are protected by FR-004 (1:1 disposition) and must never be marked stale
// (closing contract negative test).
//
// Round counting: REQ ids are monotonically numbered across the harness
// (REQ-001, REQ-002, REQ-003, ...). An angle's LastAppliedIn records the REQ
// id of the round where it was last confirmed / extended / declared /
// revived. The number of rounds elapsed since that REQ is
// (currentReqNumber - lastAppliedNumber). When this exceeds the stale
// threshold (default 3) and the angle's module is NOT in scope, the angle is
// flagged stale by flipping its Status to "stale" (and bumping the registry
// version). Auditing is best-effort: a per-module failure is reported but
// does not abort the audit, so a single corrupt registry cannot mask the
// rest of the scan.
func runAnglesAudit(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("angles audit", flag.ContinueOnError)
	flags.SetOutput(stderr)
	bindUsage(flags, "angles audit")
	root := flags.String("root", ".", "repository root")
	var modules stringsFlag
	flags.Var(&modules, "module", "module path to scan (repeatable; scans all docs/design/angles/*.yaml when omitted)")
	currentReq := flags.String("current-req", "", "current REQ id (drives round counting)")
	var scopeInclude stringsFlag
	flags.Var(&scopeInclude, "scope-include", "scope.include path (repeatable); angles in these modules are protected from stale")
	staleAfter := flags.Int("stale-after", 3, "number of rounds without disposition before stale")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if *currentReq == "" {
		fmt.Fprintln(stderr, "angles audit: --current-req is required")
		return 2
	}
	if !reqIDPatternRE.MatchString(*currentReq) {
		fmt.Fprintln(stderr, "angles audit: --current-req must match REQ-NNN")
		return 2
	}
	if *staleAfter < 1 {
		fmt.Fprintln(stderr, "angles audit: --stale-after must be >= 1")
		return 2
	}
	curNum := reqNumber(*currentReq)
	if curNum < 0 {
		fmt.Fprintf(stderr, "angles audit: --current-req %q has no numeric suffix\n", *currentReq)
		return 2
	}
	targets, err := resolveAuditModules(*root, modules)
	if err != nil {
		fmt.Fprintln(stderr, formatFailure("angles audit", err))
		return 1
	}
	scopeSet := map[string]bool{}
	for _, s := range scopeInclude {
		scopeSet[normalizeModule(s)] = true
	}
	result := auditRun{
		CurrentReq:      *currentReq,
		StaleAfter:      *staleAfter,
		ScannedModules:  []string{},
		ScopeModules:    scopeInclude,
		StaleAngles:     []staleAngleReport{},
		Errors:          []string{},
		Marked:          []string{},
		ProtectedAngles: []protectedAngleReport{},
	}
	for _, mp := range targets {
		reg, err := runtime.LoadRegistry(*root, mp)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("load %s: %v", mp, err))
			continue
		}
		result.ScannedModules = append(result.ScannedModules, mp)
		moduleIsScope := scopeSet[normalizeModule(mp)]
		mutated := false
		for i := range reg.Angles {
			a := &reg.Angles[i]
			if a.Status != runtime.AngleStatusActive {
				continue
			}
			lastNum := reqNumber(a.LastAppliedIn)
			if moduleIsScope {
				result.ProtectedAngles = append(result.ProtectedAngles, protectedAngleReport{
					Module:        mp,
					ID:            a.ID,
					LastAppliedIn: a.LastAppliedIn,
					Reason:        "in scope.include; protected by FR-004 disposition requirement",
				})
				continue
			}
			gap := curNum - lastNum
			if lastNum < 0 || gap >= *staleAfter {
				result.StaleAngles = append(result.StaleAngles, staleAngleReport{
					Module:        mp,
					ID:            a.ID,
					Statement:     a.Statement,
					Target:        a.Target,
					LastAppliedIn: a.LastAppliedIn,
					RoundsGap:     gap,
					Action:        "marked stale",
				})
				a.Status = runtime.AngleStatusStale
				mutated = true
				result.Marked = append(result.Marked, mp+":"+a.ID)
			}
		}
		// Persist any stale flips back to disk so the audit has a real
		// effect on registry state (C-008 contract: stale is recorded in
		// the registry, not only in the CLI output). Bumping the version
		// is what makes the registry an append-mostly audit log instead of
		// an in-memory report.
		if mutated {
			reg.Version = bumpRegistryVersion(reg.Version)
			if err := runtime.SaveRegistry(*root, mp, reg); err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("save %s: %v", mp, err))
			}
		}
	}
	sort.Strings(result.ScannedModules)
	sort.Strings(result.Marked)
	return encodeJSON(stdout, result)
}

// auditRun is the JSON shape of `angles audit`. Distinguishes "scanned but
// didn't change" (`ProtectedAngles`) from "actually flagged" (`StaleAngles`)
// so reviewers can audit both decisions.
type auditRun struct {
	CurrentReq      string                 `json:"current_req"`
	StaleAfter      int                    `json:"stale_after"`
	ScannedModules  []string               `json:"scanned_modules"`
	ScopeModules    []string               `json:"scope_modules"`
	StaleAngles     []staleAngleReport     `json:"stale_angles"`
	ProtectedAngles []protectedAngleReport `json:"protected_angles"`
	Errors          []string               `json:"errors,omitempty"`
	Marked          []string               `json:"marked,omitempty"`
}

type staleAngleReport struct {
	Module        string `json:"module"`
	ID            string `json:"id"`
	Statement     string `json:"statement"`
	Target        string `json:"target"`
	LastAppliedIn string `json:"last_applied_in"`
	RoundsGap     int    `json:"rounds_gap"`
	Action        string `json:"action"`
}

type protectedAngleReport struct {
	Module        string `json:"module"`
	ID            string `json:"id"`
	LastAppliedIn string `json:"last_applied_in"`
	Reason        string `json:"reason"`
}

// bumpRegistryVersion mirrors runtime.bumpVersion's logic so the CLI does
// not need to import a lowercase helper from the runtime package.
func bumpRegistryVersion(v string) string {
	if !strings.HasPrefix(v, "v") {
		return "v0.0.1"
	}
	rest := strings.TrimPrefix(v, "v")
	parts := strings.Split(rest, ".")
	if len(parts) != 3 {
		return "v0.0.1"
	}
	maj, err1 := strconv.Atoi(parts[0])
	min, err2 := strconv.Atoi(parts[1])
	pat, err3 := strconv.Atoi(parts[2])
	if err1 != nil || err2 != nil || err3 != nil {
		return "v0.0.1"
	}
	return fmt.Sprintf("v%d.%d.%d", maj, min, pat+1)
}

// resolveAuditModules collects the set of registry modules the audit pass
// should scan. When --module is omitted the pass scans every file under
// docs/design/angles/*.yaml, which is what the closing-contract negative
// test ("never marks in-scope angles stale") exercises when the scope is a
// strict subset of all known modules.
func resolveAuditModules(root string, explicit stringsFlag) ([]string, error) {
	if len(explicit) > 0 {
		out := make([]string, 0, len(explicit))
		for _, m := range explicit {
			out = append(out, normalizeModule(m))
		}
		return out, nil
	}
	dir := filepath.Join(root, "docs", "design", "angles")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []string{}, nil
		}
		return nil, fmt.Errorf("read %s: %w", dir, err)
	}
	out := []string{}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".yaml") {
			continue
		}
		out = append(out, runtime.ModuleFromRegistryFileName(name))
	}
	sort.Strings(out)
	return out, nil
}

// reqNumber extracts the numeric suffix from a REQ id; returns -1 when the
// input does not parse. Used both for round-counting and invalid-input
// detection.
func reqNumber(id string) int {
	m := reqIDPatternRE.FindStringSubmatch(id)
	if len(m) < 2 {
		return -1
	}
	n, err := strconv.Atoi(m[1])
	if err != nil {
		return -1
	}
	return n
}

// normalizeModule trims a module path's leading/trailing slashes for set
// membership comparisons in audit (scope.include paths may vary in slash
// style; the registry canonical form is without leading/trailing slashes).
func normalizeModule(p string) string {
	return strings.Trim(p, "/")
}
