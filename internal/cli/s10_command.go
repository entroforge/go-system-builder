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

// runS10Command exposes the small S10 tool surface used by the Agent at the
// point where the work is naturally produced. It deliberately has no write
// operation: validation and status make the next evidence-registration step
// explicit, while Runtime transitions remain Controller-owned.
func runS10Command(args []string, stdout, stderr io.Writer) int {
	if wantsHelp(args) {
		name := compactHelpName(args)
		if name == "" {
			name = "<status|manifest init|manifest validate|manifest render|manifest scaffold>"
		}
		printCommandHelp(stdout, "loop-harness s10 "+name, "S10 is a read-only macro audit: inspect status, validate the finite manifest, render its Markdown report, scaffold a copyable manifest/envelope shape, and route defects back through S7→S8→S9.")
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
	outcome := flags.String("outcome", "pass", "evidence outcome: pass, review_required, approved, approved_with_risk, or blocked")
	if err := flags.Parse(args[1:]); err != nil {
		return 2
	}
	if strings.TrimSpace(*file) == "" {
		fmt.Fprintln(stderr, "s10 manifest validate requires --file <manifest.json>; next: write the finite coverage_inventory and counterevidence ledger first")
		return 2
	}
	manifestPath, err := safeS10Path(*root, *file)
	if err != nil {
		fmt.Fprintf(stderr, "s10 manifest validate: %v\n", err)
		return 1
	}
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		fmt.Fprintf(stderr, "s10 manifest validate: read %s: %v; next: provide the manifest path from the current S10 assignment\n", *file, err)
		return 1
	}
	manifestType := strings.TrimSpace(*kind)
	if manifestType == "" {
		var header struct {
			ManifestType string `json:"manifest_type"`
		}
		if err := json.Unmarshal(data, &header); err != nil {
			fmt.Fprintf(stderr, "s10 manifest validate: %v\n", err)
			return 1
		}
		manifestType = header.ManifestType
	}
	summary, err := validateS10ManifestForRepository(*root, data, manifestType, strings.TrimSpace(*outcome))
	if err != nil {
		fmt.Fprintf(stderr, "s10 manifest validate: %v\n", err)
		return 1
	}
	next := "create a fingerprinted acceptance/release-audit evidence envelope pointing to this immutable manifest, then register it with `loop-harness runtime evidence add --expected-revision <N> --id <id> --kind <acceptance|release_audit> --path <envelope.json> --produced-by <agent> --responsibility <role>`; let the Controller evaluate the next gate and do not call runtime transition"
	if strings.TrimSpace(*outcome) == "blocked" {
		next = "register the blocked release-audit envelope with `loop-harness runtime evidence add`, then let the Controller take TR-018 to paused; do not call runtime transition"
	} else if strings.TrimSpace(*outcome) == "review_required" {
		next = "register the review-required acceptance envelope with `loop-harness runtime evidence add`, then let the Controller route TR-016 back to S7; do not call runtime transition"
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

// runS10ManifestInit (RC-18 F-H2) emits the missing manifest scaffold: the
// `scaffold` verb covers the evidence envelope, but the manifest itself — the
// finite coverage_inventory plus counterevidence ledger the schema requires —
// previously had to be copied out of the examples by reading source. The
// template carries the full s10-audit-manifest.schema.json required set
// (including metrics.audit_area_coverage for release_audit) with every
// agent-supplied fact as a <PLACEHOLDER>, so it can never validate or be
// registered verbatim: filling the placeholders is the work. `--emit-template
// -` writes to stdout; any other path is written exclusively (never
// overwriting an existing file). Dry-run: Runtime state is untouched.
func runS10ManifestInit(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("s10 manifest init", flag.ContinueOnError)
	flags.SetOutput(stderr)
	bindUsage(flags, "s10 manifest init")
	root := flags.String("root", ".", "repository root")
	manifestType := flags.String("type", "", "manifest type to scaffold: acceptance or release_audit")
	emitTemplate := flags.String("emit-template", "-", "write the manifest template to this repository-relative path, or `-` for stdout")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	kind := strings.TrimSpace(*manifestType)
	if kind != "acceptance" && kind != "release_audit" {
		fmt.Fprintln(stderr, "s10 manifest init requires --type acceptance or --type release_audit; next: pick the manifest the current cursor needs, then fill every <PLACEHOLDER> and run `loop-harness s10 manifest validate --file <path> --type <type>`")
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
		fmt.Fprintln(stderr, "s10 manifest init: manifest template below (stdout; dry-run, not written to disk) — replace every <PLACEHOLDER>, then validate with `loop-harness s10 manifest validate --file <path> --type "+kind+"`")
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
	fmt.Fprintf(stdout, "wrote %s manifest template to %s; replace every <PLACEHOLDER>, then validate with `loop-harness s10 manifest validate --file %s --type %s`\n", kind, target, target, kind)
	return 0
}

// s10ManifestTemplate builds the minimal-yet-complete manifest shape for the
// requested type. Fields the schema requires but only the audit can fill stay
// <PLACEHOLDER>; nothing outside the schema's additionalProperties:false set
// is emitted, so the filled template cannot be rejected for unknown fields.
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
				"oracle":        "<FALSIFYING-ORACLE>",
				"owner":         "<OWNER-ROLE>",
				"evidence_refs": []any{"<EVIDENCE-ID>"},
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

// runS10ManifestScaffold (RC-18 F-H2) writes a copyable starting shape for
// the S10 evidence envelope — the missing scaffold the manifest examples did
// not cover. The envelope is the fingerprinted artifact `runtime evidence
// add --kind acceptance|release_audit` registers, so the template carries the
// required conclusion plus the audit_manifest_path/sha256 binding to a
// manifest the caller has already validated. `--type accepted|blocked` picks
// the release-audit conclusion; every fact an Agent must supply stays a
// <PLACEHOLDER> so the scaffold can never be registered verbatim. The
// command is dry-run: it never writes Runtime state.
func runS10ManifestScaffold(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("s10 manifest scaffold", flag.ContinueOnError)
	flags.SetOutput(stderr)
	bindUsage(flags, "s10 manifest scaffold")
	kind := flags.String("type", "accepted", "conclusion to scaffold: accepted or blocked")
	manifestPath := flags.String("manifest", "s10/manifest.json", "validated S10 manifest path the envelope binds (repository-relative)")
	if err := flags.Parse(args); err != nil {
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
		"disclosure":              "dry-run scaffold only — replace every <PLACEHOLDER> with current Runtime facts, then register with `loop-harness runtime evidence add --expected-revision <N> --id <id> --kind release_audit --path <envelope.json> --produced-by <agent> --responsibility <role>`; never edit a registered envelope in place",
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
	fmt.Fprintln(stderr, "s10 manifest scaffold: envelope template below (stdout; dry-run, not written to disk)")
	_, err = stdout.Write(data)
	if err != nil {
		fmt.Fprintf(stderr, "s10 manifest scaffold: write stdout: %v\n", err)
		return 1
	}
	return 0
}

// runS10ManifestRender renders the 16-section ACC/release-audit Markdown
// from a validated manifest (RC-11 C-5: the Markdown is a projection of the
// manifest, not a second hand-maintained carrier). The manifest must pass the
// same validation the Gate runs; output goes to stdout or --output.
func runS10ManifestRender(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("s10 manifest render", flag.ContinueOnError)
	flags.SetOutput(stderr)
	bindUsage(flags, "s10 manifest render")
	root := flags.String("root", ".", "repository root")
	file := flags.String("file", "", "S10 manifest JSON path relative to repository root")
	kind := flags.String("type", "", "manifest type: acceptance or release_audit (default: read manifest_type)")
	output := flags.String("output", "", "write the Markdown to this repository-relative path instead of stdout")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if strings.TrimSpace(*file) == "" {
		fmt.Fprintln(stderr, "s10 manifest render requires --file <manifest.json>; next: validate the manifest first with `loop-harness s10 manifest validate --file <path>`")
		return 2
	}
	manifestPath, err := safeS10Path(*root, *file)
	if err != nil {
		fmt.Fprintf(stderr, "s10 manifest render: %v\n", err)
		return 1
	}
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		fmt.Fprintf(stderr, "s10 manifest render: read %s: %v; next: validate the manifest first with `loop-harness s10 manifest validate --file <path>`\n", *file, err)
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
	// A routed outcome keeps its unresolved rows by design; rendering must
	// not require a clean ledger, only the completeness the Gate enforces
	// either way. The repository-aware helper also applies the shared baseline
	// and authoritative inventory checks when the current Runtime is available.
	summary, err := validateS10ManifestForRepository(*root, data, manifestType, "review_required")
	if err != nil {
		summary, err = validateS10ManifestForRepository(*root, data, manifestType, "blocked")
	}
	if err != nil {
		fmt.Fprintf(stderr, "s10 manifest render: %v; next: fix the named rows with `loop-harness s10 manifest validate --file <path>`, then re-render\n", err)
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
	if err := flags.Parse(args); err != nil {
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
			"UNKNOWN, unsupported PASS, unowned risk, untracked debt, and blocking finding must be zero before S11",
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
	result := s10ArtifactStatus{
		State: "missing",
		Next:  "produce the " + manifestType + " manifest, validate it, then register a fingerprinted evidence envelope",
	}
	runtimeID := stringValue(state["runtime_id"])
	currentGeneration := integerValue(nestedStateValue(state, "baseline", "generation"))
	currentRound := integerValue(nestedStateValue(state, "review", "round"))
	wantedKinds := map[string]bool{manifestType: true}
	if manifestType == "acceptance" {
		wantedKinds["acceptance_record"] = true
	} else {
		wantedKinds["release_audit_record"] = true
	}
	for _, raw := range stateEvidence(state) {
		entry, _ := raw.(map[string]any)
		if entry == nil || !wantedKinds[stringValue(entry["kind"])] || stringValue(entry["status"]) != "valid" {
			continue
		}
		if entry["invalidated_by"] != nil {
			return s10InvalidArtifact(result, "evidence is invalidated; register a new current S10 evidence envelope")
		}
		if integerValue(entry["baseline_generation"]) != currentGeneration || integerValue(entry["review_round"]) != currentRound {
			return s10InvalidArtifact(result, "evidence binding is stale; baseline_generation and review_round must match the current Runtime")
		}
		result.EvidenceID = stringValue(entry["id"])
		path := stringValue(entry["path"])
		evidencePath, pathErr := safeS10Path(root, path)
		if pathErr != nil {
			return s10InvalidArtifact(result, pathErr.Error())
		}
		data, err := os.ReadFile(evidencePath)
		if err != nil {
			return s10InvalidArtifact(result, "evidence artifact unreadable: "+err.Error())
		}
		if sha256HexForArtifact(data) != stringValue(entry["sha256"]) {
			return s10InvalidArtifact(result, "evidence artifact hash mismatch; register a new immutable envelope")
		}
		var envelope struct {
			RuntimeID          string `json:"runtime_id"`
			BaselineGeneration int    `json:"baseline_generation"`
			ReviewRound        int    `json:"review_round"`
			Conclusion         string `json:"conclusion"`
			ManifestPath       string `json:"audit_manifest_path"`
			ManifestSHA        string `json:"audit_manifest_sha256"`
		}
		if err := json.Unmarshal(data, &envelope); err != nil || envelope.ManifestPath == "" || envelope.ManifestSHA == "" {
			return s10InvalidArtifact(result, "audit_manifest_path and audit_manifest_sha256 are required")
		}
		if envelope.RuntimeID != runtimeID || envelope.BaselineGeneration != currentGeneration || envelope.ReviewRound != currentRound {
			return s10InvalidArtifact(result, "evidence binding is stale; runtime_id, baseline_generation, and review_round must match the current Runtime")
		}
		result.ManifestPath = envelope.ManifestPath
		result.Conclusion = strings.TrimSpace(envelope.Conclusion)
		manifestFile, pathErr := safeS10Path(root, envelope.ManifestPath)
		if pathErr != nil {
			return s10InvalidArtifact(result, pathErr.Error())
		}
		manifestData, err := os.ReadFile(manifestFile)
		if err != nil {
			return s10InvalidArtifact(result, "manifest unreadable: "+err.Error())
		}
		if sha256HexForArtifact(manifestData) != envelope.ManifestSHA {
			return s10InvalidArtifact(result, "manifest hash mismatch; do not edit in place, regenerate and re-register")
		}
		// RC-16: status/gate single source. The same qualitygate.S10ExternalBaseline
		// builder that feeds the gate's ValidateForOutcomeWithBaseline is used here,
		// so `s10 status` and the gate can never diverge on the external denominator.
		baseline, baselineErr := qualitygate.S10ExternalBaseline(root, state, nil)
		if baselineErr != nil {
			return s10InvalidArtifact(result, "external changed-surface baseline is unverifiable: "+baselineErr.Error()+"; next: restore the current-generation completion artifacts so the changed-surface denominator can be re-derived, then re-run `s10 status`")
		}
		var summary acceptance.Summary
		if acceptance.S10AuthorityAvailable(state) {
			authority, authorityErr := acceptance.BuildS10InventoryAuthority(root, state, baseline)
			if authorityErr != nil {
				return s10InvalidArtifact(result, "authoritative inventory is unverifiable: "+authorityErr.Error()+"; next: restore the current bound REQ, contract/TASK registrations, and pinned S7 ReviewPlan")
			}
			summary, err = acceptance.ValidateForOutcomeWithBaselineAndAuthority(manifestData, manifestType, result.Conclusion, baseline, authority)
		} else {
			summary, err = acceptance.ValidateForOutcomeWithBaseline(manifestData, manifestType, result.Conclusion, baseline)
		}
		if err != nil {
			return s10InvalidArtifact(result, err.Error())
		}
		var manifestBinding struct {
			RuntimeID          string `json:"runtime_id"`
			BaselineGeneration int    `json:"baseline_generation"`
			ReviewRound        int    `json:"review_round"`
		}
		if err := json.Unmarshal(manifestData, &manifestBinding); err != nil || manifestBinding.RuntimeID != runtimeID || manifestBinding.BaselineGeneration != currentGeneration || manifestBinding.ReviewRound != currentRound {
			return s10InvalidArtifact(result, "manifest binding is stale; runtime_id, baseline_generation, and review_round must match the current Runtime")
		}
		result.InventoryCount = summary.InventoryCount
		result.CounterevidenceCount = summary.CounterevidenceCount
		result.AuditAreaCount = summary.AuditAreaCount
		result.EvidenceRefsCount = len(summary.EvidenceRefs)
		result.Metrics = summary.Metrics
		// RC-16: routed outcomes are no longer surfaced before the strict
		// reference audit — the same missingS10EvidenceRefs audit the gate
		// applies must pass for every outcome, so `s10 status` cannot declare
		// a route ready on a ledger the gate would reject.
		if missing := missingS10EvidenceRefsInStateWithSelf(root, state, result.EvidenceID, summary.EvidenceRefs); len(missing) > 0 {
			return s10InvalidArtifact(result, "manifest references evidence not registered as current valid Runtime evidence: "+strings.Join(missing, ", ")+"; ids match runtime evidence verbatim — copy them from `.claude/loop-state.json` evidence[].id; register those evidence artifacts first, then regenerate and re-register this manifest")
		}
		if result.Conclusion == "blocked" || result.Conclusion == "review_required" {
			// Routed outcomes keep their unresolved rows by design
			// (acceptance.ValidateForOutcomeWithBaseline); the route itself is
			// the actionable fact once the ledger audit passes.
			result.State = result.Conclusion
			if result.Conclusion == "blocked" {
				result.Next = "let the Controller take TR-018 to paused with the recorded blocker; do not call runtime transition"
			} else {
				result.Next = "let the Controller route TR-016 back to S7 for a fresh complete round; do not call runtime transition"
			}
			return result
		}
		result.State = "ready"
		result.Next = "let the Controller evaluate the S10 gate; do not call runtime transition or release commands"
		return result
	}
	return result
}

func s10InvalidArtifact(result s10ArtifactStatus, message string) s10ArtifactStatus {
	result.State = "invalid"
	result.Error = message
	result.Next = "correct the named S10 artifact, validate it, and register a new fingerprinted evidence envelope"
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
