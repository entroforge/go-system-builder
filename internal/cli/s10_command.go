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
	"github.com/entroforge/go-system-builder/internal/fileview"
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
			name = "<status|manifest init|manifest validate|manifest render|manifest scaffold|envelope lint>"
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
	case "envelope":
		if len(args) > 1 && args[1] == "lint" {
			return runS10EnvelopeLint(args[2:], stdout, stderr)
		}
		fmt.Fprintln(stderr, "s10 envelope requires lint")
		return 2
	default:
		fmt.Fprintln(stderr, "s10 requires <status|manifest>")
		return 2
	}
}

func runS10Manifest(args []string, stdout, stderr io.Writer) int {
	if wantsHelp(args) {
		printCommandHelp(stdout, "loop-harness s10 manifest <init|validate|render|scaffold>", "Use --help on the concrete operation for its flags; envelope lint validates a proposed registration.")
		return 0
	}
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
	if err := parseWorkspaceFlags(flags, args[1:]); err != nil {
		return flagParseExitCode(err)
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
	next := "create a fingerprinted acceptance/release-audit evidence envelope pointing to this immutable manifest, then register it with `loop-harness runtime evidence add --id <id> --kind <acceptance|release_audit> --path <envelope.json> --produced-by <agent> --responsibility <role>`; let the Controller evaluate the next gate and do not call runtime transition"
	if strings.TrimSpace(*outcome) == "blocked" {
		next = "register the blocked release-audit envelope with `loop-harness runtime evidence add`, then let the Controller take TR-018 to paused; do not call runtime transition"
	} else if strings.TrimSpace(*outcome) == "review_required" {
		next = "register the review-required acceptance envelope with `loop-harness runtime evidence add`, then let the Controller route TR-016 back to S7; do not call runtime transition"
	}
	validationScope := "runtime_authority"
	if state, _ := readOptionalS10State(*root); state == nil {
		validationScope = "author_only"
	}
	return encodeJSON(stdout, map[string]any{
		"validation_scope":      validationScope,
		"transition_ready":      false,
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
	if err := parseWorkspaceFlags(flags, args); err != nil {
		return flagParseExitCode(err)
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
	legacy := flags.String("type", "", "deprecated: accepted maps to release_audit/approved; blocked maps to release_audit/blocked")
	kind := flags.String("kind", "", "envelope kind: acceptance or release_audit")
	outcome := flags.String("outcome", "", "explicit outcome; defaults to an unfinished placeholder")
	manifestPath := flags.String("manifest", ".claude/evidence/s10/manifest.json", "validated S10 manifest path the envelope binds (repository-relative)")
	if err := parseWorkspaceFlags(flags, args); err != nil {
		return flagParseExitCode(err)
	}
	if *legacy != "" {
		if *kind != "" || *outcome != "" || (*legacy != "accepted" && *legacy != "blocked") {
			fmt.Fprintln(stderr, "deprecated --type cannot be combined with --kind/--outcome; use an explicit kind and outcome")
			return 2
		}
		*kind = "release_audit"
		*outcome = "approved"
		if *legacy == "blocked" {
			*outcome = "blocked"
		}
		fmt.Fprintln(stderr, "deprecated --type: use --kind release_audit --outcome "+*outcome)
	}
	contract, err := acceptance.Contract(*kind)
	if err != nil {
		fmt.Fprintln(stderr, "s10 manifest scaffold requires --kind acceptance or --kind release_audit")
		return 2
	}
	conclusion := *outcome
	if conclusion == "" {
		conclusion = "<OUTCOME>"
	} else if _, ok := contract.Outcomes[conclusion]; !ok {
		fmt.Fprintln(stderr, "illegal outcome for "+contract.Kind)
		return 2
	}
	manifest := strings.TrimSpace(*manifestPath)
	envelope := map[string]any{
		"schema_version":          "1.0.0",
		"evidence_id":             "<EVIDENCE-ID>",
		"kind":                    contract.Kind,
		"runtime_id":              "<RUNTIME-ID>",
		"baseline_generation":     "<BASELINE-GENERATION-INT>",
		"review_round":            "<REVIEW-ROUND-INT>",
		"producer_agent_id":       "<PRODUCER-AGENT-ID>",
		"producer_responsibility": contract.Responsibilities[0],
		"subject_refs":            []any{},
		"conclusion":              conclusion,
		"audit_manifest_path":     manifest,
		"audit_manifest_sha256":   "<SHA256-OF-MANIFEST-FILE>",
		"disclosure":              "dry-run scaffold only — replace every <PLACEHOLDER> with current Runtime facts, then register with `loop-harness runtime evidence add --id <id> --kind release_audit --path <envelope.json> --produced-by <agent> --responsibility <role>`; never edit a registered envelope in place",
	}
	if event := contract.Outcomes[conclusion]; event != "" {
		envelope["requested_event"] = event
	}
	envelope["disclosure"] = "unfinished scaffold: supply current facts and independent verification; register with --kind " + contract.Kind

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
	if err := parseWorkspaceFlags(flags, args); err != nil {
		return flagParseExitCode(err)
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
	if err := parseWorkspaceFlags(flags, args); err != nil {
		return flagParseExitCode(err)
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
	result := s10ArtifactStatus{State: "missing", Next: "produce the manifest and register the current " + manifestType + " envelope"}
	row, err := acceptance.SelectS10Candidate(state, manifestType)
	if err != nil {
		for _, raw := range stateEvidence(state) {
			row, _ := raw.(map[string]any)
			if row["kind"] == manifestType {
				result.EvidenceID = stringValue(row["id"])
				return s10InvalidArtifact(result, "no admissible current candidate; evidence binding or responsibility is stale/invalid")
			}
		}
		result.Error = err.Error()
		return result
	}
	result.EvidenceID = stringValue(row["id"])
	files, err := fileview.ForState(root, state)
	if err != nil {
		return s10InvalidArtifact(result, err.Error())
	}
	candidate, err := acceptance.ValidateS10Candidate(acceptance.CandidateInput{State: state, Files: files, Kind: manifestType, EvidenceID: result.EvidenceID})
	result.ManifestPath = candidate.Envelope.ManifestPath
	result.Conclusion = candidate.Envelope.Conclusion
	if err != nil {
		return s10InvalidArtifact(result, err.Error())
	}
	result.InventoryCount = candidate.Summary.InventoryCount
	result.CounterevidenceCount = candidate.Summary.CounterevidenceCount
	result.AuditAreaCount = candidate.Summary.AuditAreaCount
	result.EvidenceRefsCount = len(candidate.Summary.EvidenceRefs)
	result.Metrics = candidate.Summary.Metrics
	result.State = "ready"
	result.Next = "artifact valid; the Controller must separately verify the complete round and transition requirements"
	if result.Conclusion == "blocked" || result.Conclusion == "review_required" {
		result.State = result.Conclusion
		result.Next = "let the Controller route the recorded blocker (TR-018 for blocked, TR-016/TR-031 for review_required); do not force a transition"
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
	files, err := fileview.ForState(root, state)
	if err != nil {
		return acceptance.Summary{}, err
	}
	baseline, err := acceptance.BuildS10ExternalBaselineWithFiles(files, state, nil)
	if err != nil {
		return acceptance.Summary{}, fmt.Errorf("external changed-surface baseline is unverifiable: %w", err)
	}
	authority, err := acceptance.BuildS10InventoryAuthorityWithFiles(files, state, baseline)
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

// runS10EnvelopeLint validates a proposed registration against the current
// authority. It reads no Store, acquires no lock, and writes no Runtime event.
func runS10EnvelopeLint(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("s10 envelope lint", flag.ContinueOnError)
	flags.SetOutput(stderr)
	bindUsage(flags, flags.Name())
	root := flags.String("root", ".", "repository root")
	path := flags.String("file", "", "proposed evidence envelope path")
	if err := parseWorkspaceFlags(flags, args); err != nil {
		return flagParseExitCode(err)
	}
	state, err := readOptionalS10State(*root)
	if err != nil || state == nil {
		fmt.Fprintln(stderr, "envelope lint requires a readable bound Runtime:", err)
		return 1
	}
	files, err := fileview.ForState(*root, state)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	data, err := files.ReadFile(*path)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	var envelope acceptance.Envelope
	if err := json.Unmarshal(data, &envelope); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	rows := stateEvidence(state)
	for _, raw := range rows {
		row, _ := raw.(map[string]any)
		if row["id"] == envelope.EvidenceID {
			fmt.Fprintln(stderr, "proposed evidence ID is already registered; lint a new correction ID")
			return 1
		}
	}
	state["evidence"] = append(rows, map[string]any{"id": envelope.EvidenceID, "kind": envelope.Kind, "path": *path, "sha256": sha256HexForArtifact(data), "status": "valid", "baseline_generation": integerValue(nestedStateValue(state, "baseline", "generation")), "review_round": envelope.ReviewRound, "produced_by": []string{envelope.ProducerAgentID}, "responsibility_id": envelope.ProducerResponsibility})
	result, err := acceptance.ValidateS10Candidate(acceptance.CandidateInput{State: state, Files: files, Kind: envelope.Kind, EvidenceID: envelope.EvidenceID})
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if err := files.Verify(); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return encodeJSON(stdout, map[string]any{"artifact_valid": true, "transition_ready": false, "evidence_id": result.EvidenceID, "observed_revision": result.ObservedRevision, "consumed": result.Consumed, "selection": "legacy_append", "next": "register this exact envelope; registration revalidates it inside the revision CAS"})
}
