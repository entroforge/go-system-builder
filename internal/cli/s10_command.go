package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/entroforge/go-system-builder/internal/acceptance"
	"github.com/entroforge/go-system-builder/internal/evidence"
	"github.com/entroforge/go-system-builder/internal/qualitygate"
	"github.com/entroforge/go-system-builder/internal/runtime"
)

// runS10Command retains explicit diagnostic interfaces. Normal authors fill
// and save the exact current Hook questionnaire; its original consumer records
// the report and handles this step's outlet, without a second envelope.
func runS10Command(args []string, stdout, stderr io.Writer) int {
	if wantsHelp(args) {
		name := compactHelpName(args)
		if name == "" {
			name = "<status|manifest init|manifest validate|manifest render|manifest scaffold>"
		}
		printCommandHelp(stdout, "loop-harness s10 "+name, "S10 diagnostics inspect existing records or show legacy shapes. For normal work, fill the existing fields at the exact Manifest path supplied by the current Hook and save that file; the system records it and handles this step. Diagnostics do not supply the author conclusion or require manual evidence registration.")
		return 0
	}
	if len(args) == 0 {
		fmt.Fprintln(stderr, "s10 requires <status|manifest>")
		return 2
	}
	switch args[0] {
	case "status":
		return runS10Status(args[1:], stdout, stderr)
	case "manifest":
		return runS10Manifest(args[1:], stdout, stderr)
	default:
		fmt.Fprintln(stderr, "s10 requires <status|manifest>")
		return 2
	}
}

func runS10Manifest(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || (args[0] != "validate" && args[0] != "render" && args[0] != "scaffold" && args[0] != "init") {
		fmt.Fprintln(stderr, "s10 manifest requires <init|validate|render|scaffold>")
		return 2
	}
	if args[0] == "scaffold" {
		return runS10ManifestScaffold(args[1:], stdout, stderr)
	}
	if args[0] == "init" {
		return runS10ManifestInit(args[1:], stdout, stderr)
	}
	if args[0] == "render" {
		return runS10ManifestRender(args[1:], stdout, stderr)
	}
	flags := flag.NewFlagSet("s10 manifest validate", flag.ContinueOnError)
	flags.SetOutput(stderr)
	bindUsage(flags, "s10 manifest validate")
	root := flags.String("root", ".", "repository root")
	file := flags.String("file", "", "S10 manifest JSON path relative to repository root")
	kind := flags.String("type", "", "manifest type: acceptance or release_audit (default: read manifest_type)")
	outcome := flags.String("outcome", "pass", "diagnostic outcome argument: pass, review_required, approved, approved_with_risk, or blocked; does not fill the questionnaire conclusion")
	if err := parseWorkspaceFlags(flags, args[1:]); err != nil {
		return 2
	}
	if strings.TrimSpace(*file) == "" {
		fmt.Fprintln(stderr, "s10 manifest validate requires --file <manifest.json>; use the exact current Hook Manifest path. Normal authors fill its existing professional fields and save; no separate ledger or envelope is required.")
		return 2
	}
	manifestPath, err := safeS10Path(*root, *file)
	if err != nil {
		fmt.Fprintf(stderr, "s10 manifest validate: %v\n", err)
		return 1
	}
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		fmt.Fprintf(stderr, "s10 manifest validate: read %s: %v; next: read the exact Manifest path supplied by the current Hook and save that existing questionnaire\n", *file, err)
		return 1
	}
	manifestType := strings.TrimSpace(*kind)
	if manifestType == "" {
		var header struct {
			ManifestType string `json:"manifest_type"`
		}
		if err := json.Unmarshal(data, &header); err != nil {
			fmt.Fprintf(stderr, "s10 manifest validate %s: %v; next: correct the reported fields in this file; normal submission uses the exact current Hook Manifest and its save receiver\n", *file, err)
			return 1
		}
		manifestType = header.ManifestType
	}
	summary, err := validateS10ManifestForRepository(*root, data, manifestType, strings.TrimSpace(*outcome))
	if err != nil {
		fmt.Fprintf(stderr, "s10 manifest validate %s: %v; next: correct the reported fields in this file; normal submission uses the exact current Hook Manifest and its save receiver\n", *file, err)
		return 1
	}
	next := "This diagnostic did not submit a report or choose its conclusion. Fill the existing fields at the exact Manifest path supplied by the current Hook and save it; the system records the professional report and handles this step. Do not create or manually register an envelope."
	if strings.TrimSpace(*outcome) == "blocked" {
		next = "Record the actual blocked conclusion and reason in the current Hook Manifest and save it; the system records that report and handles its original blocker route. This diagnostic did not submit it; no manual envelope registration is needed."
	} else if strings.TrimSpace(*outcome) == "review_required" {
		next = "Record the actual review_required conclusion and unfinished work in the current Hook Manifest and save it; the system records that report and handles its original return route. This diagnostic did not submit it; no manual envelope registration is needed."
	}
	return encodeJSON(stdout, map[string]any{
		"valid":                 true,
		"manifest_type":         summary.ManifestType,
		"outcome":               strings.TrimSpace(*outcome),
		"inventory_count":       summary.InventoryCount,
		"dispositioned_count":   summary.DispositionedCount,
		"counterevidence_count": summary.CounterevidenceCount,
		"audit_area_count":      summary.AuditAreaCount,
		"metrics":               summary.Metrics,
		"next":                  next,
	})
}

// runS10ManifestInit retains a diagnostic template emitter. Normal authors use
// the already prepared current questionnaire instead of copying this legacy
// shape, its machine metadata or placeholder metrics into another report.
func runS10ManifestInit(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("s10 manifest init", flag.ContinueOnError)
	flags.SetOutput(stderr)
	bindUsage(flags, "s10 manifest init")
	root := flags.String("root", ".", "repository root")
	manifestType := flags.String("type", "", "manifest type to scaffold: acceptance or release_audit")
	emitTemplate := flags.String("emit-template", "-", "write the manifest template to this repository-relative path, or `-` for stdout")
	if err := parseWorkspaceFlags(flags, args); err != nil {
		return 2
	}
	kind := strings.TrimSpace(*manifestType)
	if kind != "acceptance" && kind != "release_audit" {
		fmt.Fprintln(stderr, "s10 manifest init requires --type acceptance or --type release_audit for a diagnostic template. For normal work, fill and save the exact current Hook Manifest; initialization is not an author prerequisite.")
		return 2
	}
	template := s10ManifestTemplate(kind)
	data, err := json.MarshalIndent(template, "", "  ")
	if err != nil {
		fmt.Fprintf(stderr, "s10 manifest init: %v\n", err)
		return 1
	}
	data = append(data, '\n')
	target := strings.TrimSpace(*emitTemplate)
	if target == "-" || target == "" {
		fmt.Fprintln(stderr, "s10 manifest init: legacy diagnostic template below (stdout; no Runtime change). For normal "+kind+" work, fill and save the existing exact Hook questionnaire; do not copy its machine metadata or create another report.")
		if _, err := stdout.Write(data); err != nil {
			fmt.Fprintf(stderr, "s10 manifest init: write stdout: %v\n", err)
			return 1
		}
		return 0
	}
	targetPath, err := safeS10Path(*root, target)
	if err != nil {
		fmt.Fprintf(stderr, "s10 manifest init: %v\n", err)
		return 1
	}
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		fmt.Fprintf(stderr, "s10 manifest init: create template directory: %v\n", err)
		return 1
	}
	file, err := os.OpenFile(targetPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		fmt.Fprintf(stderr, "s10 manifest init: %s already exists or is not writable: %v; never overwrite a manifest in place — pick a new path or edit the existing file\n", target, err)
		return 1
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		_ = os.Remove(targetPath)
		fmt.Fprintf(stderr, "s10 manifest init: write %s: %v\n", target, err)
		return 1
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(targetPath)
		fmt.Fprintf(stderr, "s10 manifest init: close %s: %v\n", target, err)
		return 1
	}
	fmt.Fprintf(stdout, "wrote diagnostic %s template to %s; normal work uses the exact current Hook Manifest and its existing fields. Save that questionnaire; no separate envelope or manual registration is needed.\n", kind, target)
	return 0
}

// s10ManifestTemplate preserves the legacy diagnostic shape. Placeholder
// metadata and metrics are not normal author obligations.
func s10ManifestTemplate(manifestType string) map[string]any {
	metrics := map[string]any{
		"requirement_coverage":   "<COVERAGE-0-TO-1>",
		"contract_coverage":      "<COVERAGE-0-TO-1>",
		"changed_path_coverage":  "<COVERAGE-0-TO-1>",
		"unknown_count":          "<COUNT>",
		"unsupported_pass_count": "<COUNT>",
		"unowned_risk_count":     "<COUNT>",
		"untracked_debt_count":   "<COUNT>",
		"blocking_finding_count": "<COUNT>",
	}
	if manifestType == "release_audit" {
		metrics["audit_area_coverage"] = "<COVERAGE-0-TO-1>"
	}
	return map[string]any{
		"schema_version":      "1.0.0",
		"manifest_type":       manifestType,
		"runtime_id":          "<RUNTIME-ID>",
		"baseline_generation": "<BASELINE-GENERATION-INT>",
		"review_round":        "<REVIEW-ROUND-INT>",
		"coverage_inventory": []any{
			map[string]any{
				"id":            "<INVENTORY-ID>",
				"category":      "<requirement|contract|changed_path>",
				"source_refs":   []any{"<SOURCE-REF>"},
				"expected":      "<EXPECTED-CLAIM>",
				"oracle":        "<OPTIONAL-JUDGMENT-OR-OBSERVATION>",
				"owner":         "<OWNER-ROLE>",
				"evidence_refs": []any{"<EXISTING-FILE-OR-OUTPUT-IF-AVAILABLE>"},
				"disposition":   "<pass|not_applicable|unknown|fail>",
			},
		},
		"counterevidence": []any{
			map[string]any{
				"id":            "<COUNTEREVIDENCE-ID>",
				"inventory_id":  "<INVENTORY-ID>",
				"question":      "<WHAT-WOULD-PROVE-THE-CLAIM-FALSE>",
				"evidence_refs": []any{"<EVIDENCE-ID>"},
				"outcome":       "<pass|not_applicable|unknown|fail>",
			},
		},
		"audit_areas":       []any{},
		"risks":             []any{},
		"technical_debt":    []any{},
		"blocking_findings": []any{},
		"metrics":           metrics,
	}
}

// runS10ManifestScaffold preserves a diagnostic envelope example for explicit
// compatibility work. Normal questionnaire saves already supply this internal
// record; authors do not construct or register it.
func runS10ManifestScaffold(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("s10 manifest scaffold", flag.ContinueOnError)
	flags.SetOutput(stderr)
	bindUsage(flags, "s10 manifest scaffold")
	kind := flags.String("type", "accepted", "conclusion to scaffold: accepted or blocked")
	manifestPath := flags.String("manifest", "s10/manifest.json", "manifest path shown by the legacy diagnostic example (repository-relative)")
	if err := parseWorkspaceFlags(flags, args); err != nil {
		return 2
	}
	conclusion := strings.TrimSpace(*kind)
	if conclusion != "accepted" && conclusion != "blocked" {
		fmt.Fprintln(stderr, "s10 manifest scaffold requires --type accepted or --type blocked (the conclusion recorded in the envelope; use `--outcome` on manifest validate for review_required)")
		return 2
	}
	manifest := strings.TrimSpace(*manifestPath)
	envelope := map[string]any{
		"schema_version":          "1.0.0",
		"evidence_id":             "<EVIDENCE-ID>",
		"kind":                    "release_audit",
		"runtime_id":              "<RUNTIME-ID>",
		"baseline_generation":     "<BASELINE-GENERATION-INT>",
		"review_round":            "<REVIEW-ROUND-INT>",
		"producer_agent_id":       "<PRODUCER-AGENT-ID>",
		"producer_responsibility": "<Release Auditor|Acceptance>",
		"subject_refs":            []any{},
		"conclusion":              conclusion,
		"audit_manifest_path":     manifest,
		"audit_manifest_sha256":   "<SHA256-OF-MANIFEST-FILE>",
		"disclosure":              "Legacy diagnostic example only, not a normal author task. Fill the existing fields at the exact current Hook Manifest path and save it; the system supplies machine metadata, records the report and handles this step. Do not copy this envelope or manually register evidence.",
	}
	if conclusion == "accepted" {
		envelope["conclusion"] = "approved"
		envelope["disclosure"] = strings.Replace(envelope["disclosure"].(string), "--kind release_audit", "--kind release_audit (or --kind acceptance for the S10 acceptance envelope)", 1)
	}
	data, err := json.MarshalIndent(envelope, "", "  ")
	if err != nil {
		fmt.Fprintf(stderr, "s10 manifest scaffold: %v\n", err)
		return 1
	}
	data = append(data, '\n')
	fmt.Fprintln(stderr, "s10 manifest scaffold: legacy diagnostic example below (stdout; no Runtime change). Normal authors save the exact current Hook questionnaire, not this envelope.")
	_, err = stdout.Write(data)
	if err != nil {
		fmt.Fprintf(stderr, "s10 manifest scaffold: write stdout: %v\n", err)
		return 1
	}
	return 0
}

// runS10ManifestRender is the retained explicit diagnostic renderer. Normal
// completion uses the existing RenderMarkdown projection at its save outlet;
// authors do not maintain a second report or run this CLI to submit one.
func runS10ManifestRender(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("s10 manifest render", flag.ContinueOnError)
	flags.SetOutput(stderr)
	bindUsage(flags, "s10 manifest render")
	root := flags.String("root", ".", "repository root")
	file := flags.String("file", "", "S10 manifest JSON path relative to repository root")
	kind := flags.String("type", "", "manifest type: acceptance or release_audit (default: read manifest_type)")
	output := flags.String("output", "", "write the Markdown to this repository-relative path instead of stdout")
	if err := parseWorkspaceFlags(flags, args); err != nil {
		return 2
	}
	if strings.TrimSpace(*file) == "" {
		fmt.Fprintln(stderr, "s10 manifest render requires --file <manifest.json>; next: use the exact current Hook Manifest path; normal work is filled and saved there, without a separate diagnostic step")
		return 2
	}
	manifestPath, err := safeS10Path(*root, *file)
	if err != nil {
		fmt.Fprintf(stderr, "s10 manifest render: %v\n", err)
		return 1
	}
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		fmt.Fprintf(stderr, "s10 manifest render: read %s: %v; next: use the exact current Hook Manifest path; normal work is filled and saved there, without a separate diagnostic step\n", *file, err)
		return 1
	}
	manifestType := strings.TrimSpace(*kind)
	if manifestType == "" {
		var header struct {
			ManifestType string `json:"manifest_type"`
		}
		if err := json.Unmarshal(data, &header); err != nil {
			fmt.Fprintf(stderr, "s10 manifest render: %v\n", err)
			return 1
		}
		manifestType = header.ManifestType
	}
	// Keep this legacy diagnostic path separate from the normal save outlet.
	summary, err := validateS10ManifestForRepository(*root, data, manifestType, "review_required")
	if err != nil {
		summary, err = validateS10ManifestForRepository(*root, data, manifestType, "blocked")
	}
	if err != nil {
		fmt.Fprintf(stderr, "s10 manifest render: %v; next: normal authors continue the exact current Hook questionnaire and save its professional fields; this diagnostic does not register or submit a report\n", err)
		return 1
	}
	var manifest acceptance.Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		fmt.Fprintf(stderr, "s10 manifest render: decode manifest: %v\n", err)
		return 1
	}
	rendered := acceptance.RenderMarkdown(manifest, summary)
	if strings.TrimSpace(*output) == "" {
		fmt.Fprint(stdout, rendered)
		return 0
	}
	outputPath, err := safeS10Path(*root, *output)
	if err != nil {
		fmt.Fprintf(stderr, "s10 manifest render: %v\n", err)
		return 1
	}
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		fmt.Fprintf(stderr, "s10 manifest render: create output directory: %v\n", err)
		return 1
	}
	if err := os.WriteFile(outputPath, []byte(rendered), 0o644); err != nil {
		fmt.Fprintf(stderr, "s10 manifest render: write %s: %v\n", *output, err)
		return 1
	}
	fmt.Fprintf(stdout, "rendered %s manifest Markdown to %s\n", manifestType, *output)
	return 0
}

type s10StatusProjection struct {
	Stage          string            `json:"stage"`
	LifecycleState string            `json:"lifecycle_state"`
	ReviewRound    int               `json:"review_round"`
	Acceptance     s10ArtifactStatus `json:"acceptance"`
	ReleaseAudit   s10ArtifactStatus `json:"release_audit"`
	Next           string            `json:"next"`
	Guardrails     []string          `json:"guardrails"`
}

type s10ArtifactStatus struct {
	State                string             `json:"state"`
	EvidenceID           string             `json:"evidence_id,omitempty"`
	Conclusion           string             `json:"conclusion,omitempty"`
	ManifestPath         string             `json:"manifest_path,omitempty"`
	InventoryCount       int                `json:"inventory_count,omitempty"`
	CounterevidenceCount int                `json:"counterevidence_count,omitempty"`
	AuditAreaCount       int                `json:"audit_area_count,omitempty"`
	EvidenceRefsCount    int                `json:"evidence_refs_count,omitempty"`
	Metrics              acceptance.Metrics `json:"metrics,omitempty"`
	Error                string             `json:"error,omitempty"`
	Next                 string             `json:"next"`
}

func runS10Status(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("s10 status", flag.ContinueOnError)
	flags.SetOutput(stderr)
	bindUsage(flags, "s10 status")
	root := flags.String("root", ".", "repository root")
	if err := parseWorkspaceFlags(flags, args); err != nil {
		return 2
	}
	snapshot, err := runtime.NewStore(
		filepath.Join(*root, ".claude/loop-state.json"),
		filepath.Join(*root, ".claude/loop-events.jsonl"),
	).Snapshot()
	if err != nil {
		fmt.Fprintf(stderr, "s10 status: read runtime: %v; next: initialize or recover the Runtime before starting S10\n", err)
		return 1
	}
	state := snapshot.State
	lifecycle := lifecycleState(state)
	stage, _, _ := projectNext(lifecycle, lifecyclePhase(state), *root)
	result := s10StatusProjection{
		Stage:          stage,
		LifecycleState: lifecycle,
		ReviewRound:    integerValue(nestedStateValue(state, "review", "round")),
		Acceptance:     inspectS10Artifact(*root, state, "acceptance"),
		ReleaseAudit:   inspectS10Artifact(*root, state, "release_audit"),
		Guardrails: []string{
			"S9 has no direct S10 exit; every repair must return through a fresh S7 clean round",
			"S10 is read-only for product code, locked REQ, contracts, and TASKs",
			"Main owns the professional conclusion and actual remaining work; displayed metrics do not certify coverage or evidence sufficiency",
		},
	}
	switch lifecycle {
	case "acceptance":
		result.Next = result.Acceptance.Next
	case "release_audit":
		result.Next = result.ReleaseAudit.Next
	default:
		result.Next = "S10 status is observational; current cursor is " + lifecycle + ", follow `loop-harness next --root <root>`"
	}
	return encodeJSON(stdout, result)
}

func inspectS10Artifact(root string, state map[string]any, manifestType string) s10ArtifactStatus {
	result := s10ArtifactStatus{State: "missing"}
	path, pathErr := auditQuestionnairePath(root, state, manifestType)
	if pathErr == nil {
		result.ManifestPath = path
		result.Next = fmt.Sprintf("Continue the current %s questionnaire at %s; Main supplies any missing responsibility materials.", manifestType, filepath.Join(root, filepath.FromSlash(path)))
	} else {
		result.Next = "Main must resolve the current questionnaire path: " + pathErr.Error()
	}
	var selected map[string]any
	for _, raw := range stateEvidence(state) {
		entry, _ := raw.(map[string]any)
		kind := stringValue(entry["kind"])
		if (kind == manifestType || kind == manifestType+"_record") && stringValue(entry["status"]) == "valid" &&
			integerValue(entry["baseline_generation"]) == integerValue(nestedStateValue(state, "baseline", "generation")) &&
			integerValue(entry["review_round"]) == integerValue(nestedStateValue(state, "review", "round")) && entry["invalidated_by"] == nil {
			selected = entry
		}
	}
	if selected == nil {
		return result
	}
	result.EvidenceID = stringValue(selected["id"])
	envelopePath, err := safeS10Path(root, stringValue(selected["path"]))
	if err != nil {
		return s10InvalidArtifact(result, err.Error())
	}
	data, err := os.ReadFile(envelopePath)
	if err != nil {
		return s10InvalidArtifact(result, "recorded report unreadable: "+err.Error())
	}
	var envelope struct {
		ManifestPath string `json:"audit_manifest_path"`
		Conclusion   string `json:"conclusion"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return s10InvalidArtifact(result, err.Error())
	}
	if envelope.ManifestPath == "" {
		return s10InvalidArtifact(result, "the recorded report has no questionnaire path")
	}
	result.ManifestPath = envelope.ManifestPath
	manifestPath, err := safeS10Path(root, result.ManifestPath)
	if err != nil {
		return s10InvalidArtifact(result, err.Error())
	}
	data, err = os.ReadFile(manifestPath)
	if err != nil {
		return s10InvalidArtifact(result, "questionnaire unreadable: "+err.Error())
	}
	var manifest acceptance.Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return s10InvalidArtifact(result, err.Error())
	}
	if manifest.ManifestType != manifestType || manifest.RuntimeID != stringValue(state["runtime_id"]) ||
		manifest.BaselineGeneration != integerValue(nestedStateValue(state, "baseline", "generation")) ||
		manifest.ReviewRound != integerValue(nestedStateValue(state, "review", "round")) {
		return s10InvalidArtifact(result, "this questionnaire belongs to another responsibility or review round")
	}
	result.Conclusion = strings.TrimSpace(envelope.Conclusion)
	result.InventoryCount = len(manifest.CoverageInventory)
	result.CounterevidenceCount = len(manifest.Counterevidence)
	result.AuditAreaCount = len(manifest.AuditAreas)
	result.Metrics = acceptance.DeriveMetrics(manifest) // Display only.
	for _, item := range manifest.CoverageInventory {
		result.EvidenceRefsCount += len(item.EvidenceRefs)
	}
	if result.Conclusion == "review_required" || result.Conclusion == "blocked" {
		result.State = result.Conclusion
		result.Next = fmt.Sprintf("The author reported %s in %s; follow the original recorded return/blocker responsibility.", result.Conclusion, manifestPath)
		return result
	}
	var pending []string
	for _, item := range manifest.CoverageInventory {
		if item.Disposition == "" || item.Disposition == "unknown" || item.Disposition == "fail" || item.Disposition == "blocked" {
			pending = append(pending, fmt.Sprintf("%s (%s): %s", item.Expected, item.ID, item.Disposition))
		}
	}
	for _, item := range manifest.Counterevidence {
		if item.Outcome != "pass" && item.Outcome != "not_applicable" {
			pending = append(pending, item.Question+": "+item.Outcome)
		}
	}
	for _, item := range manifest.AuditAreas {
		if item.Conclusion != "pass" && item.Conclusion != "not_applicable" {
			pending = append(pending, item.ID+": "+item.Conclusion)
		}
	}
	for _, item := range manifest.BlockingFindings {
		pending = append(pending, fmt.Sprintf("reported blocker: %+v", item))
	}
	if result.Conclusion == "" {
		pending = append(pending, "the author has not supplied the overall conclusion")
	}
	if (manifestType == "acceptance" && result.Conclusion != "pass") || (manifestType == "release_audit" && result.Conclusion != "approved" && result.Conclusion != "approved_with_risk") {
		pending = append(pending, "the author's conclusion is "+result.Conclusion)
	}
	if len(pending) != 0 {
		result.State = "pending"
		result.Next = fmt.Sprintf("Main continues %s at %s: %s. This report has not completed the current step.", manifestType, manifestPath, strings.Join(pending, "; "))
		return result
	}
	result.State = "ready"
	result.Next = fmt.Sprintf("The author recorded %s at %s; the normal save outlet performs this step's original transition and supplies the next responsibility.", result.Conclusion, manifestPath)
	return result
}

func s10InvalidArtifact(result s10ArtifactStatus, message string) s10ArtifactStatus {
	result.State = "invalid"
	result.Error = message
	result.Next = "Main must resolve the specific read/path issue above at the current questionnaire; do not create a second envelope"
	return result
}

func stateEvidence(state map[string]any) []any {
	items, _ := state["evidence"].([]any)
	return items
}

// missingS10EvidenceRefsInState mirrors the gate's evidence-reference audit
// (qualitygate.missingS10EvidenceRefs): an id only counts as available when
// the registered entry is valid, current-generation, SHA-verified, kind-registered,
// round-bound, and not the envelope's own self-proof. Execution anchors (://)
// never satisfy S10 manifest refs. Keeping both consumers identical prevents
// `s10 status` from declaring ready on a ledger the gate would reject
// (2026-08-28 walkthrough defect C; RC-14 phantom/self-proof).
func missingS10EvidenceRefsInState(root string, state map[string]any, refs []string) []string {
	return missingS10EvidenceRefsInStateWithSelf(root, state, "", refs)
}

func missingS10EvidenceRefsInStateWithSelf(root string, state map[string]any, selfID string, refs []string) []string {
	currentGeneration := integerValue(nestedStateValue(state, "baseline", "generation"))
	currentRound := integerValue(nestedStateValue(state, "review", "round"))
	available := make(map[string]struct{})
	for _, raw := range stateEvidence(state) {
		entry, _ := raw.(map[string]any)
		if entry == nil || stringValue(entry["status"]) != "valid" || integerValue(entry["baseline_generation"]) != currentGeneration {
			continue
		}
		if v := entry["invalidated_by"]; v != nil {
			if str, ok := v.(string); ok {
				if stringValue(str) != "" {
					continue
				}
			} else {
				continue
			}
		}
		id := stringValue(entry["id"])
		if id == "" || id == selfID {
			continue
		}
		if currentRound > 0 {
			if r := integerValue(entry["review_round"]); r != 0 && r != currentRound {
				continue
			}
		}
		kind := stringValue(entry["kind"])
		if kind != "" && !evidence.DefaultCatalog().IsRegisteredKind(kind) {
			continue
		}
		path := stringValue(entry["path"])
		if path == "" {
			continue
		}
		full, pathErr := safeS10Path(root, path)
		if pathErr != nil {
			continue
		}
		data, err := os.ReadFile(full)
		if err != nil || sha256HexForArtifact(data) != stringValue(entry["sha256"]) {
			continue
		}
		available[id] = struct{}{}
	}
	missing := make([]string, 0)
	for _, ref := range refs {
		if stringValue(ref) == "" {
			missing = append(missing, ref)
			continue
		}
		if containsExecutionAnchor(ref) {
			missing = append(missing, ref)
			continue
		}
		if _, ok := available[ref]; !ok {
			missing = append(missing, ref)
		}
	}
	return missing
}

func containsExecutionAnchor(ref string) bool {
	return strings.Contains(ref, "://")
}

func nestedStateValue(state map[string]any, parent, child string) any {
	nested, _ := state[parent].(map[string]any)
	return nested[child]
}

func readOptionalS10State(root string) (map[string]any, error) {
	path := filepath.Join(root, ".claude", "loop-state.json")
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read S10 Runtime state %s: %w", path, err)
	}
	var state map[string]any
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("decode S10 Runtime state %s: %w", path, err)
	}
	return state, nil
}

func validateS10ManifestForRepository(root string, data []byte, manifestType, outcome string) (acceptance.Summary, error) {
	state, err := readOptionalS10State(root)
	if err != nil {
		return acceptance.Summary{}, err
	}
	if state == nil {
		return acceptance.ValidateForOutcome(data, manifestType, outcome)
	}
	baseline, err := qualitygate.S10ExternalBaseline(root, state, nil)
	if err != nil {
		return acceptance.Summary{}, fmt.Errorf("external changed-surface baseline is unverifiable: %w", err)
	}
	if !acceptance.S10AuthorityAvailable(state) {
		return acceptance.ValidateForOutcomeWithBaseline(data, manifestType, outcome, baseline)
	}
	authority, err := acceptance.BuildS10InventoryAuthority(root, state, baseline)
	if err != nil {
		return acceptance.Summary{}, fmt.Errorf("authoritative inventory is unverifiable: %w", err)
	}
	return acceptance.ValidateForOutcomeWithBaselineAndAuthority(data, manifestType, outcome, baseline, authority)
}

func safeS10Path(root, value string) (string, error) {
	clean := filepath.Clean(value)
	if filepath.IsAbs(clean) || clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("S10 path must stay inside the repository: %q", value)
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("resolve S10 repository root: %w", err)
	}
	resolvedRoot, err := filepath.EvalSymlinks(rootAbs)
	if err != nil {
		return "", fmt.Errorf("resolve S10 repository root: %w", err)
	}
	candidate := filepath.Join(rootAbs, clean)
	resolvedPath, err := filepath.EvalSymlinks(candidate)
	if err != nil {
		// A missing file will be reported by the caller. It is still safe to
		// return the lexical candidate because no external bytes can be read.
		if os.IsNotExist(err) {
			return candidate, nil
		}
		return "", fmt.Errorf("resolve S10 path %q: %w", value, err)
	}
	relative, err := filepath.Rel(resolvedRoot, resolvedPath)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("S10 path must stay inside the repository: %q", value)
	}
	return candidate, nil
}
