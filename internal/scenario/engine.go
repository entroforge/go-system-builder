package scenario

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/entroforge/go-system-builder/internal/schema"
)

const (
	prototypeRoot = "docs/design/prototypes"
	modelFile     = "scenario-model.json"
	fixtureFile   = "fixture-contract.json"
	casesFile     = "cases.json"
	coverageFile  = "scenario-coverage.json"
)

var (
	moduleNamePattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
	storyRefPattern   = regexp.MustCompile(`^S-[0-9]{3}$`)
	caseIDPattern     = regexp.MustCompile(`^CASE-[A-Z0-9]+(-[A-Z0-9]+)*$`)
	flowRefPattern    = regexp.MustCompile(`^(?:F-[0-9]{3}|PATH-[A-Za-z0-9-]+)$`)
)

type sourcePackage struct {
	directory       string
	model           ScenarioModel
	fixtures        FixtureContract
	crossMatrix     CrossMatrix
	modelBytes      []byte
	fixtureBytes    []byte
	crossMatrixByte []byte
	stories         []byte
	flows           []byte
	root            string
}

// GenerateModule validates the module's source package and atomically writes
// its two generated current outputs.
func GenerateModule(root, module string) (Report, error) {
	source, err := loadSourcePackage(root, module)
	if err != nil {
		return Report{}, err
	}
	outputs, err := buildOutputs(source)
	if err != nil {
		return Report{}, err
	}
	if err := writeOutputsAtomically(source.directory, outputs.casesBytes, outputs.coverageBytes); err != nil {
		return Report{}, err
	}
	return buildReport(source, outputs, root)
}

// ValidateModule validates source and confirms generated outputs are exactly
// the bytes that the current source package would produce.
func ValidateModule(root, module string, options ValidateOptions) (Report, error) {
	source, err := loadSourcePackage(root, module)
	if err != nil {
		return Report{}, err
	}
	outputs, err := buildOutputs(source)
	if err != nil {
		return Report{}, err
	}
	if err := validateCurrentOutputs(source.directory, outputs); err != nil {
		return Report{}, err
	}
	if err := validateSpecs(root, module, outputs.cases, options); err != nil {
		return Report{}, err
	}
	return buildReport(source, outputs, root)
}

// ValidateAll validates only actual module directories immediately below the
// prototypes root. Root-level README and template files are not modules.
func ValidateAll(root string, options ValidateOptions) ([]Report, error) {
	base := filepath.Join(root, prototypeRoot)
	if _, err := os.Lstat(base); errors.Is(err, os.ErrNotExist) {
		return []Report{}, nil
	} else if err != nil {
		return nil, fmt.Errorf("inspect prototypes root: %w", err)
	}
	if _, _, err := securePrototypesRoot(root); err != nil {
		return nil, err
	}
	reports := make([]Report, 0)
	entries, err := os.ReadDir(base)
	if errors.Is(err, os.ErrNotExist) {
		return []Report{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read prototypes root: %w", err)
	}
	var modules []string
	for _, entry := range entries {
		if entry.Name() == "template" || entry.Name() == "templates" {
			continue
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return reports, fmt.Errorf("module directory %s is a symlink", entry.Name())
		}
		if !entry.IsDir() {
			continue
		}
		modules = append(modules, entry.Name())
	}
	sort.Strings(modules)
	reports = make([]Report, 0, len(modules))
	for _, module := range modules {
		report, validateErr := ValidateModule(root, module, options)
		if validateErr != nil {
			return reports, fmt.Errorf("validate module %s: %w", module, validateErr)
		}
		reports = append(reports, report)
	}
	if _, err := RunBridge(root, true); err != nil {
		return reports, err
	}
	return reports, nil
}

func loadSourcePackage(root, module string) (sourcePackage, error) {
	if err := validateModuleName(module); err != nil {
		return sourcePackage{}, err
	}
	directory, _, err := secureModuleDirectory(root, module)
	if err != nil {
		return sourcePackage{}, err
	}
	if err := recoverTransaction(directory); err != nil {
		return sourcePackage{}, fmt.Errorf("recover scenario outputs: %w", err)
	}
	modelBytes, err := readRequiredFile(directory, modelFile)
	if err != nil {
		return sourcePackage{}, err
	}
	fixtureBytes, err := readRequiredFile(directory, fixtureFile)
	if err != nil {
		return sourcePackage{}, err
	}
	stories, err := readRequiredFile(directory, "stories.md")
	if err != nil {
		return sourcePackage{}, err
	}
	flows, err := readRequiredFile(directory, "flows.md")
	if err != nil {
		return sourcePackage{}, err
	}
	if err := validateSchema("scenario-model.schema.json", modelBytes); err != nil {
		return sourcePackage{}, fmt.Errorf("scenario-model.json schema: %w", err)
	}
	if err := validateSchema("scenario-fixture-contract.schema.json", fixtureBytes); err != nil {
		return sourcePackage{}, fmt.Errorf("fixture-contract.json schema: %w", err)
	}
	var model ScenarioModel
	if err := decodeStrict(modelBytes, &model); err != nil {
		return sourcePackage{}, fmt.Errorf("decode scenario-model.json: %w", err)
	}
	var fixtures FixtureContract
	if err := decodeStrict(fixtureBytes, &fixtures); err != nil {
		return sourcePackage{}, fmt.Errorf("decode fixture-contract.json: %w", err)
	}
	crossMatrixBytes, err := readRequiredFile(directory, "cross-matrix.json")
	if err != nil {
		return sourcePackage{}, err
	}
	if err := validateSchema("scenario-cross-matrix.schema.json", crossMatrixBytes); err != nil {
		return sourcePackage{}, fmt.Errorf("cross-matrix.json schema: %w", err)
	}
	crossMatrix, err := decodeCrossMatrix(crossMatrixBytes)
	if err != nil {
		return sourcePackage{}, err
	}
	source := sourcePackage{
		directory: directory, model: model, fixtures: fixtures, crossMatrix: crossMatrix,
		modelBytes: modelBytes, fixtureBytes: fixtureBytes, crossMatrixByte: crossMatrixBytes,
		stories: stories, flows: flows, root: root,
	}
	if err := validateSource(source, module, root); err != nil {
		return sourcePackage{}, err
	}
	return source, nil
}

func validateSource(source sourcePackage, module string, root string) error {
	if source.model.Module != module || source.fixtures.Module != module {
		return fmt.Errorf("module mismatch: expected %q", module)
	}
	if len(source.model.Facts) == 0 || len(source.model.Rules) == 0 {
		return fmt.Errorf("module must declare at least one fact and one rule")
	}
	if !validProfile(source.model.CoverageProfile) {
		return fmt.Errorf("unsupported coverage_profile %q", source.model.CoverageProfile)
	}
	if err := validateCrossMatrix(source, root); err != nil {
		return err
	}
	idRegistry := map[string]string{}
	partitions := map[string]map[string]struct{}{}
	for _, fact := range source.model.Facts {
		if err := registerID(idRegistry, fact.ID, "fact"); err != nil {
			return err
		}
		if len(fact.Partitions) == 0 {
			return fmt.Errorf("fact %q has no partitions", fact.ID)
		}
		partitionIDs := map[string]struct{}{}
		for _, partition := range fact.Partitions {
			if err := registerID(idRegistry, partition.ID, "partition"); err != nil {
				return err
			}
			if _, exists := partitionIDs[partition.ID]; exists {
				return fmt.Errorf("duplicate partition id %q in fact %q", partition.ID, fact.ID)
			}
			partitionIDs[partition.ID] = struct{}{}
		}
		partitions[fact.ID] = partitionIDs
	}

	fixtureIDs, err := validateFixtures(source.fixtures, idRegistry)
	if err != nil {
		return err
	}
	counts := Counts{Facts: len(source.model.Facts), Rules: len(source.model.Rules)}
	usedFixtures := map[string]struct{}{}
	caseIDs := map[string]struct{}{}
	for _, rule := range source.model.Rules {
		if err := registerID(idRegistry, rule.ID, "rule"); err != nil {
			return err
		}
		if len(rule.SourceRefs) == 0 || hasEmpty(rule.SourceRefs) {
			return fmt.Errorf("rule %q must have non-empty source_refs", rule.ID)
		}
		if len(rule.Branches) == 0 {
			return fmt.Errorf("rule %q has no branches", rule.ID)
		}
		for _, branch := range rule.Branches {
			if err := validateBranch(branch, rule, partitions, fixtureIDs, source.stories, source.flows); err != nil {
				return fmt.Errorf("rule %s branch %s: %w", rule.ID, branch.ID, err)
			}
			if err := registerID(idRegistry, branch.ID, "branch"); err != nil {
				return err
			}
			if !caseIDPattern.MatchString(branch.CaseID) {
				return fmt.Errorf("case id %q must match %s (the case id is the S2→S7 verification denominator — L2 single-denominator rule)", branch.CaseID, caseIDPattern.String())
			}
			if _, exists := caseIDs[branch.CaseID]; exists {
				return fmt.Errorf("duplicate case id %q", branch.CaseID)
			}
			caseIDs[branch.CaseID] = struct{}{}
			if _, exists := idRegistry[branch.CaseID]; exists {
				return fmt.Errorf("duplicate id %q", branch.CaseID)
			}
			idRegistry[branch.CaseID] = "case"
			usedFixtures[branch.FixtureID] = struct{}{}
			counts.Branches++
			if branch.Polarity == "positive" {
				counts.Positive++
			} else {
				counts.Negative++
			}
			if branch.Required {
				counts.RequiredBranches++
			}
			if branch.BrowserRequired {
				counts.BrowserRequiredCases++
			}
		}
	}
	if err := validateRatio(source.model.CoverageProfile, counts.Positive, counts.Negative); err != nil {
		return err
	}
	if len(usedFixtures) != len(fixtureIDs) {
		return fmt.Errorf("fixture contract contains unreferenced fixture")
	}
	return nil
}

func validateFixtures(contract FixtureContract, ids map[string]string) (map[string]struct{}, error) {
	if len(contract.Fixtures) == 0 {
		return nil, fmt.Errorf("fixture contract must declare fixtures")
	}
	fixtureIDs := make(map[string]struct{}, len(contract.Fixtures))
	for _, fixture := range contract.Fixtures {
		if err := registerID(ids, fixture.ID, "fixture"); err != nil {
			return nil, err
		}
		if _, exists := fixtureIDs[fixture.ID]; exists {
			return nil, fmt.Errorf("duplicate fixture id %q", fixture.ID)
		}
		fixtureIDs[fixture.ID] = struct{}{}
		if fixture.Persona == "" || !fixture.Synthetic || len(fixture.Setup) == 0 || len(fixture.Cleanup) == 0 {
			return nil, fmt.Errorf("fixture %q must be synthetic with persona, setup and cleanup", fixture.ID)
		}
		if hasEmpty(fixture.Setup) || hasEmpty(fixture.Cleanup) {
			return nil, fmt.Errorf("fixture %q has empty setup or cleanup step", fixture.ID)
		}
	}
	return fixtureIDs, nil
}

func validateBranch(branch Branch, rule Rule, partitions map[string]map[string]struct{}, fixtureIDs map[string]struct{}, stories, flows []byte) error {
	if branch.ID == "" || branch.CaseID == "" || branch.Title == "" || branch.FixtureID == "" {
		return fmt.Errorf("id, case_id, title and fixture_id are required")
	}
	if branch.Polarity != "positive" && branch.Polarity != "negative" {
		return fmt.Errorf("polarity must be positive or negative")
	}
	if len(branch.Witness) == 0 {
		return fmt.Errorf("witness must not be empty")
	}
	for factID, partitionID := range branch.Witness {
		partitionIDs, exists := partitions[factID]
		if !exists {
			return fmt.Errorf("witness references unknown fact %q", factID)
		}
		if _, exists := partitionIDs[partitionID]; !exists {
			return fmt.Errorf("witness references unknown partition %q for fact %q", partitionID, factID)
		}
	}
	if _, exists := fixtureIDs[branch.FixtureID]; !exists {
		return fmt.Errorf("references unknown fixture %q", branch.FixtureID)
	}
	if len(branch.StoryRefs) == 0 || len(branch.FlowRefs) == 0 {
		return fmt.Errorf("story_refs and flow_refs must not be empty — story_refs needs at least one S-NNN (exactly three digits) and flow_refs needs at least one F-NNN and one PATH-* entry")
	}
	for _, ref := range branch.StoryRefs {
		if !storyRefPattern.MatchString(ref) || !markdownHeadingContainsID(stories, ref) {
			return fmt.Errorf("story reference %q is missing from stories.md", ref)
		}
	}
	for _, ref := range branch.FlowRefs {
		if !flowRefPattern.MatchString(ref) || !markdownHeadingContainsID(flows, ref) {
			return fmt.Errorf("flow reference %q is missing from flows.md", ref)
		}
	}
	if branch.BrowserRequired && !hasPathReference(branch.FlowRefs) {
		return fmt.Errorf("browser-required branch must reference at least one PATH-* entry in flow_refs (the Playwright route the case walks)")
	}
	if err := validateCommonOracle(branch.Oracle); err != nil {
		return err
	}
	if branch.Polarity == "positive" {
		return nil
	}
	if branch.Oracle.Rejection == "" || branch.Oracle.ExpectedState == "" || branch.Oracle.Recovery == "" {
		return fmt.Errorf("negative oracle requires rejection, expected_state and recovery")
	}
	if strings.EqualFold(strings.TrimSpace(branch.Oracle.Recovery), "n/a") && (len(branch.Oracle.RecoverySourceRefs) == 0 || branch.Oracle.RecoveryReason == "") {
		return fmt.Errorf("negative oracle N/A recovery requires recovery_source_refs and recovery_reason")
	}
	for _, ref := range branch.Oracle.RecoverySourceRefs {
		if !contains(rule.SourceRefs, ref) {
			return fmt.Errorf("recovery source reference %q is not a rule source_ref", ref)
		}
	}
	return nil
}

func validateCommonOracle(oracle Oracle) error {
	if len(oracle.Visible) == 0 || oracle.TerminalState == "" || len(oracle.PersistedEffects) == 0 || len(oracle.ForbiddenSideEffects) == 0 {
		return fmt.Errorf("oracle requires visible, terminal_state, persisted_effects and forbidden_side_effects")
	}
	return nil
}

func validateRatio(profile string, positive, negative int) error {
	if positive == 0 {
		return fmt.Errorf("coverage ratio requires at least one positive branch")
	}
	minimum := minimumNegativeRatio(profile)
	if float64(negative)/float64(positive) < minimum {
		return fmt.Errorf("coverage ratio negative:positive %.2f is below required %.2f for %s", float64(negative)/float64(positive), minimum, profile)
	}
	return nil
}

func buildOutputs(source sourcePackage) (builtOutputs, error) {
	if err := validateSource(source, source.model.Module, source.root); err != nil {
		return builtOutputs{}, err
	}
	var cases []Case
	for _, rule := range source.model.Rules {
		for _, branch := range rule.Branches {
			cases = append(cases, Case{
				ID: branch.CaseID, RuleID: rule.ID, BranchID: branch.ID,
				Title: branch.Title, Polarity: branch.Polarity, Required: branch.Required,
				Witness: cloneWitness(branch.Witness), Oracle: branch.Oracle,
				FixtureID: branch.FixtureID, StoryRefs: cloneStrings(branch.StoryRefs),
				FlowRefs: cloneStrings(branch.FlowRefs), BrowserRequired: branch.BrowserRequired,
			})
		}
	}
	sort.Slice(cases, func(i, j int) bool { return cases[i].ID < cases[j].ID })
	counts := countCases(source.model, cases)
	coverage := branchCoverage(cases)
	coverageOutput := CoverageOutput{
		Module: source.model.Module, CoverageProfile: source.model.CoverageProfile,
		Counts: counts, RequiredBranchCoverage: coverage,
		Ratio: ratioFor(counts, source.model.CoverageProfile),
	}
	casesOutput := CasesOutput{Module: source.model.Module, Cases: cases}
	casesBytes, err := marshalStable(casesOutput)
	if err != nil {
		return builtOutputs{}, fmt.Errorf("marshal cases output: %w", err)
	}
	coverageBytes, err := marshalStable(coverageOutput)
	if err != nil {
		return builtOutputs{}, fmt.Errorf("marshal coverage output: %w", err)
	}
	return builtOutputs{cases: cases, casesBytes: casesBytes, coverageBytes: coverageBytes, counts: counts, coverage: coverage, ratio: coverageOutput.Ratio}, nil
}

type builtOutputs struct {
	cases         []Case
	casesBytes    []byte
	coverageBytes []byte
	counts        Counts
	coverage      BranchCoverage
	ratio         Ratio
}

func validateCurrentOutputs(directory string, expected builtOutputs) error {
	casesBytes, err := readRequiredFile(directory, casesFile)
	if err != nil {
		return err
	}
	coverageBytes, err := readRequiredFile(directory, coverageFile)
	if err != nil {
		return err
	}
	if err := validateSchema("scenario-cases.schema.json", casesBytes); err != nil {
		return fmt.Errorf("cases.json schema: %w", err)
	}
	if err := validateSchema("scenario-coverage.schema.json", coverageBytes); err != nil {
		return fmt.Errorf("scenario-coverage.json schema: %w", err)
	}
	var currentCases CasesOutput
	if err := decodeStrict(casesBytes, &currentCases); err != nil {
		return fmt.Errorf("decode cases.json: %w", err)
	}
	var currentCoverage CoverageOutput
	if err := decodeStrict(coverageBytes, &currentCoverage); err != nil {
		return fmt.Errorf("decode scenario-coverage.json: %w", err)
	}
	if !bytes.Equal(casesBytes, expected.casesBytes) || !bytes.Equal(coverageBytes, expected.coverageBytes) {
		return fmt.Errorf("generated outputs are stale or tampered")
	}
	return nil
}

func buildReport(source sourcePackage, outputs builtOutputs, root string) (Report, error) {
	coverage, err := inspectSpecs(root, source.model.Module, outputs.cases)
	if err != nil {
		return Report{}, err
	}
	return Report{
		Module:              source.model.Module,
		Counts:              outputs.counts,
		Coverage:            outputs.coverage,
		Ratio:               outputs.ratio,
		BrowserSpecCoverage: coverage,
		InputFingerprint:    fingerprint(source.modelBytes, source.fixtureBytes, source.stories, source.flows),
		OutputFingerprints: OutputFingerprints{
			Cases: fingerprint(outputs.casesBytes), Coverage: fingerprint(outputs.coverageBytes),
		},
	}, nil
}

func validateSpecs(root, module string, cases []Case, options ValidateOptions) error {
	coverage, err := inspectSpecs(root, module, cases)
	if err != nil {
		return err
	}
	required := options.RequireSpecs
	if !required && options.AutoSpecs {
		if _, statErr := os.Stat(filepath.Join(root, "web", "e2e", module)); statErr == nil {
			required = true
		}
	}
	if !required {
		return nil
	}
	if coverage.RequiredCases != coverage.CoveredCases || coverage.RequiredPaths != coverage.CoveredPaths {
		return fmt.Errorf("browser spec coverage incomplete: cases %d/%d paths %d/%d", coverage.CoveredCases, coverage.RequiredCases, coverage.CoveredPaths, coverage.RequiredPaths)
	}
	return nil
}

func inspectSpecs(root, module string, cases []Case) (BrowserSpecCoverage, error) {
	requiredCasePaths := map[string][]string{}
	requiredPaths := map[string]struct{}{}
	for _, currentCase := range cases {
		if !currentCase.BrowserRequired {
			continue
		}
		casePaths := make([]string, 0, len(currentCase.FlowRefs))
		for _, ref := range currentCase.FlowRefs {
			if strings.HasPrefix(ref, "PATH-") {
				requiredPaths[ref] = struct{}{}
				casePaths = append(casePaths, ref)
			}
		}
		requiredCasePaths[currentCase.ID] = casePaths
	}
	bodies, err := readPlaywrightTestBodies(root, module)
	if err != nil {
		return BrowserSpecCoverage{}, err
	}
	coveredCases := 0
	for caseID, paths := range requiredCasePaths {
		for _, body := range bodies {
			if containsExactID(body, caseID) && containsAllExactIDs(body, paths) {
				coveredCases++
				break
			}
		}
	}
	coveredPaths := 0
	for pathID := range requiredPaths {
		for _, body := range bodies {
			if containsExactID(body, pathID) {
				coveredPaths++
				break
			}
		}
	}
	return BrowserSpecCoverage{
		RequiredCases: len(requiredCasePaths), CoveredCases: coveredCases,
		CasePercentage: percentage(coveredCases, len(requiredCasePaths)),
		RequiredPaths:  len(requiredPaths), CoveredPaths: coveredPaths,
		PathPercentage: percentage(coveredPaths, len(requiredPaths)),
	}, nil
}

func readPlaywrightTestBodies(root, module string) ([]string, error) {
	var bodies []string
	specRoot := filepath.Join(root, "web/e2e", module)
	if err := filepath.WalkDir(specRoot, func(path string, entry os.DirEntry, walkErr error) error {
		if errors.Is(walkErr, os.ErrNotExist) {
			return nil
		}
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("browser spec path %s is a symlink", path)
		}
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".spec.ts") {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return fmt.Errorf("read browser spec %s: %w", path, readErr)
		}
		bodies = append(bodies, playwrightTestBodies(string(data))...)
		return nil
	}); err != nil {
		return nil, fmt.Errorf("scan browser specs: %w", err)
	}
	return bodies, nil
}

func playwrightTestBodies(source string) []string {
	cleaned := stripJavaScriptComments(source)
	masked := maskJavaScriptStrings(cleaned)
	bindings := playwrightTestBindings(cleaned, masked)
	if len(bindings) == 0 {
		return nil
	}
	var bodies []string
	for index := 0; index < len(cleaned); index++ {
		binding := identifierAt(masked, index)
		if _, imported := bindings[binding]; !imported {
			continue
		}
		open := skipSpaces(masked, index+len(binding))
		if open >= len(masked) || masked[open] != '(' {
			continue
		}
		close := matchingDelimiter(cleaned, open, '(', ')')
		if close < 0 {
			continue
		}
		if body, found := playwrightCallbackBody(cleaned[open+1 : close]); found {
			bodies = append(bodies, body)
		}
		index = close
	}
	return bodies
}

func playwrightTestBindings(source, masked string) map[string]struct{} {
	bindings := map[string]struct{}{}
	for index := 0; index < len(masked); index++ {
		if !hasIdentifierAt(masked, index, "import") || !linePrefixIsWhitespace(masked, index) {
			continue
		}
		open := skipSpaces(masked, index+len("import"))
		if open >= len(masked) || masked[open] != '{' {
			continue
		}
		close := matchingDelimiter(source, open, '{', '}')
		if close < 0 {
			continue
		}
		from := skipSpaces(masked, close+1)
		if !hasIdentifierAt(masked, from, "from") {
			continue
		}
		moduleStart := skipSpaces(source, from+len("from"))
		moduleName, _, ok := javascriptStringAt(source, moduleStart)
		if !ok || moduleName != "@playwright/test" {
			continue
		}
		for _, imported := range strings.Split(source[open+1:close], ",") {
			fields := strings.Fields(imported)
			switch {
			case len(fields) == 1 && fields[0] == "test":
				bindings["test"] = struct{}{}
			case len(fields) == 3 && fields[0] == "test" && fields[1] == "as" && validJavaScriptIdentifier(fields[2]):
				bindings[fields[2]] = struct{}{}
			}
		}
		index = close
	}
	if len(bindings) > 0 && playwrightBindingsShadowed(masked, bindings) {
		return nil
	}
	return bindings
}

func playwrightCallbackBody(call string) (string, bool) {
	arguments := splitTopLevelArguments(call)
	if len(arguments) != 2 && len(arguments) != 3 {
		return "", false
	}
	if !staticJavaScriptString(arguments[0]) {
		return "", false
	}
	callback := arguments[1]
	if len(arguments) == 3 {
		if !staticJavaScriptObject(arguments[1]) {
			return "", false
		}
		callback = arguments[2]
	}
	if body, found := arrowCallbackBody(callback); found {
		return body, true
	}
	if body, found := functionCallbackBody(callback); found {
		return body, true
	}
	return "", false
}

func staticJavaScriptString(argument string) bool {
	argument = strings.TrimSpace(argument)
	_, end, ok := javascriptStringAt(argument, 0)
	return ok && strings.TrimSpace(argument[end+1:]) == ""
}

func staticJavaScriptObject(argument string) bool {
	argument = strings.TrimSpace(argument)
	if argument == "" || argument[0] != '{' {
		return false
	}
	end := matchingDelimiter(argument, 0, '{', '}')
	return end >= 0 && strings.TrimSpace(argument[end+1:]) == ""
}

// playwrightBindingsShadowed deliberately fails closed for files that declare
// an imported test binding in any local declaration or function-like scope.
// This is narrower and safer than trying to execute a partial TypeScript
// parser: a potentially shadowed binding cannot contribute browser evidence.
func playwrightBindingsShadowed(masked string, bindings map[string]struct{}) bool {
	for _, keyword := range []string{"const", "let", "var", "function", "class"} {
		for index := 0; index < len(masked); index++ {
			if !hasIdentifierAt(masked, index, keyword) {
				continue
			}
			start := skipSpaces(masked, index+len(keyword))
			if keyword == "const" || keyword == "let" || keyword == "var" {
				if variableDeclarationShadowsBinding(masked, start, bindings) {
					return true
				}
				continue
			}
			if keyword == "function" && start < len(masked) && masked[start] == '*' {
				start = skipSpaces(masked, start+1)
			}
			end := declarationHeaderEnd(masked, start)
			if containsAnyExactID(masked[start:end], bindings) {
				return true
			}
		}
	}
	for index := 0; index < len(masked); index++ {
		if hasIdentifierAt(masked, index, "catch") {
			start := skipSpaces(masked, index+len("catch"))
			if start < len(masked) && masked[start] == '(' {
				end := matchingDelimiter(masked, start, '(', ')')
				if end >= 0 && containsAnyExactID(masked[start+1:end], bindings) {
					return true
				}
			}
		}
	}
	for index := 0; index < len(masked); index++ {
		if hasIdentifierAt(masked, index, "function") {
			open := strings.IndexByte(masked[index+len("function"):], '(')
			if open >= 0 {
				open += index + len("function")
				close := matchingDelimiter(masked, open, '(', ')')
				if close >= 0 && containsAnyExactID(masked[open+1:close], bindings) {
					return true
				}
			}
		}
	}
	for arrow := 0; arrow+1 < len(masked); arrow++ {
		if masked[arrow:arrow+2] != "=>" {
			continue
		}
		end := arrow - 1
		for end >= 0 && isJavaScriptWhitespace(masked[end]) {
			end--
		}
		if end < 0 {
			continue
		}
		start := end
		if masked[end] == ')' {
			start = matchingOpeningDelimiter(masked, end, '(', ')')
			if start < 0 {
				continue
			}
			if containsAnyExactID(masked[start+1:end], bindings) {
				return true
			}
			continue
		}
		for start >= 0 && javascriptIdentifierChar(masked[start]) {
			start--
		}
		if containsAnyExactID(masked[start+1:end+1], bindings) {
			return true
		}
	}
	return false
}

// variableDeclarationShadowsBinding checks every declarator in a const/let/var
// statement. It deliberately examines only binding positions, not initializer
// expressions, so ordinary references to the imported Playwright binding do
// not make an otherwise safe file ambiguous.
func variableDeclarationShadowsBinding(source string, start int, bindings map[string]struct{}) bool {
	parentheses, braces, brackets := 0, 0, 0
	expectDeclarator := true
	for index := start; index < len(source); index++ {
		if expectDeclarator {
			index = skipSpaces(source, index)
			if index >= len(source) {
				return false
			}
			switch source[index] {
			case '{', '[':
				closing := byte('}')
				if source[index] == '[' {
					closing = ']'
				}
				end := matchingDelimiter(source, index, source[index], closing)
				if end < 0 {
					return false
				}
				if containsAnyExactID(source[index+1:end], bindings) {
					return true
				}
				index = end
				expectDeclarator = false
				continue
			default:
				if !javascriptIdentifierStart(source[index]) {
					expectDeclarator = false
					continue
				}
				end := index + 1
				for end < len(source) && javascriptIdentifierChar(source[end]) {
					end++
				}
				if _, shadows := bindings[source[index:end]]; shadows {
					return true
				}
				index = end - 1
				expectDeclarator = false
				continue
			}
		}

		switch source[index] {
		case '(':
			parentheses++
		case ')':
			if parentheses == 0 && braces == 0 && brackets == 0 {
				return false
			}
			if parentheses > 0 {
				parentheses--
			}
		case '{':
			braces++
		case '}':
			if braces == 0 {
				return false
			}
			braces--
		case '[':
			brackets++
		case ']':
			if brackets == 0 {
				return false
			}
			brackets--
		case ',':
			if parentheses == 0 && braces == 0 && brackets == 0 {
				expectDeclarator = true
			}
		case ';':
			if parentheses == 0 && braces == 0 && brackets == 0 {
				return false
			}
		}
	}
	return false
}

func declarationHeaderEnd(source string, start int) int {
	parentheses, braces, brackets := 0, 0, 0
	for index := start; index < len(source); index++ {
		switch source[index] {
		case '(':
			parentheses++
		case ')':
			if parentheses == 0 {
				return index
			}
			parentheses--
		case '{':
			braces++
		case '}':
			if braces == 0 {
				return index
			}
			braces--
		case '[':
			brackets++
		case ']':
			if brackets == 0 {
				return index
			}
			brackets--
		case '=', ';', '\n':
			if parentheses == 0 && braces == 0 && brackets == 0 {
				return index
			}
		}
	}
	return len(source)
}

func containsAnyExactID(source string, ids map[string]struct{}) bool {
	for id := range ids {
		if containsExactID(source, id) {
			return true
		}
	}
	return false
}

func matchingOpeningDelimiter(source string, close int, opening, closing byte) int {
	depth := 0
	for index := close; index >= 0; index-- {
		switch source[index] {
		case closing:
			depth++
		case opening:
			depth--
			if depth == 0 {
				return index
			}
		}
	}
	return -1
}

func isJavaScriptWhitespace(value byte) bool {
	return value == ' ' || value == '\t' || value == '\n' || value == '\r'
}

func splitTopLevelArguments(call string) []string {
	var arguments []string
	start := 0
	parentheses, braces, brackets := 0, 0, 0
	for index := 0; index < len(call); index++ {
		if quote := call[index]; quote == '\'' || quote == '"' || quote == '`' {
			index = skipJavaScriptString(call, index, quote)
			continue
		}
		switch call[index] {
		case '(':
			parentheses++
		case ')':
			parentheses--
		case '{':
			braces++
		case '}':
			braces--
		case '[':
			brackets++
		case ']':
			brackets--
		case ',':
			if parentheses == 0 && braces == 0 && brackets == 0 {
				arguments = append(arguments, strings.TrimSpace(call[start:index]))
				start = index + 1
			}
		}
	}
	arguments = append(arguments, strings.TrimSpace(call[start:]))
	return arguments
}

func arrowCallbackBody(argument string) (string, bool) {
	masked := maskJavaScriptStrings(argument)
	for arrow := 0; arrow+1 < len(masked); arrow++ {
		if masked[arrow:arrow+2] != "=>" {
			continue
		}
		bodyStart := skipSpaces(masked, arrow+2)
		if bodyStart >= len(masked) || masked[bodyStart] != '{' {
			continue
		}
		bodyEnd := matchingDelimiter(argument, bodyStart, '{', '}')
		if bodyEnd < 0 || strings.TrimSpace(argument[bodyEnd+1:]) != "" {
			continue
		}
		return argument[bodyStart+1 : bodyEnd], true
	}
	return "", false
}

func functionCallbackBody(argument string) (string, bool) {
	masked := maskJavaScriptStrings(argument)
	function := indexOfIdentifier(masked, "function")
	if function < 0 || strings.TrimSpace(masked[:function]) != "" && strings.TrimSpace(masked[:function]) != "async" {
		return "", false
	}
	bodyStart := strings.Index(masked[function+len("function"):], "{")
	if bodyStart < 0 {
		return "", false
	}
	bodyStart += function + len("function")
	bodyEnd := matchingDelimiter(argument, bodyStart, '{', '}')
	if bodyEnd < 0 || strings.TrimSpace(argument[bodyEnd+1:]) != "" {
		return "", false
	}
	return argument[bodyStart+1 : bodyEnd], true
}

func matchingDelimiter(source string, start int, opening, closing byte) int {
	depth := 0
	for index := start; index < len(source); index++ {
		if quote := source[index]; quote == '\'' || quote == '"' || quote == '`' {
			index = skipJavaScriptString(source, index, quote)
			continue
		}
		switch source[index] {
		case opening:
			depth++
		case closing:
			depth--
			if depth == 0 {
				return index
			}
		}
	}
	return -1
}

func stripJavaScriptComments(source string) string {
	var builder strings.Builder
	for index := 0; index < len(source); index++ {
		if source[index] == '\'' || source[index] == '"' || source[index] == '`' {
			end := skipJavaScriptString(source, index, source[index])
			builder.WriteString(source[index : end+1])
			index = end
			continue
		}
		if index+1 < len(source) && source[index] == '/' && source[index+1] == '/' {
			builder.WriteByte(' ')
			builder.WriteByte(' ')
			index += 2
			for index < len(source) && source[index] != '\n' {
				builder.WriteByte(' ')
				index++
			}
			if index < len(source) {
				builder.WriteByte('\n')
			}
			continue
		}
		if index+1 < len(source) && source[index] == '/' && source[index+1] == '*' {
			builder.WriteByte(' ')
			builder.WriteByte(' ')
			index += 2
			for index < len(source) {
				if index+1 < len(source) && source[index] == '*' && source[index+1] == '/' {
					builder.WriteByte(' ')
					builder.WriteByte(' ')
					index++
					break
				}
				if source[index] == '\n' {
					builder.WriteByte('\n')
				} else {
					builder.WriteByte(' ')
				}
				index++
			}
			continue
		}
		builder.WriteByte(source[index])
	}
	return builder.String()
}

func maskJavaScriptStrings(source string) string {
	var builder strings.Builder
	for index := 0; index < len(source); index++ {
		if source[index] != '\'' && source[index] != '"' && source[index] != '`' {
			builder.WriteByte(source[index])
			continue
		}
		quote := source[index]
		builder.WriteByte(' ')
		for index++; index < len(source); index++ {
			if source[index] == '\\' {
				builder.WriteByte(' ')
				if index+1 < len(source) {
					builder.WriteByte(' ')
					index++
				}
				continue
			}
			if source[index] == quote {
				builder.WriteByte(' ')
				break
			}
			if source[index] == '\n' {
				builder.WriteByte('\n')
			} else {
				builder.WriteByte(' ')
			}
		}
	}
	return builder.String()
}

func skipJavaScriptString(source string, start int, quote byte) int {
	for index := start + 1; index < len(source); index++ {
		if source[index] == '\\' {
			index++
			continue
		}
		if source[index] == quote {
			return index
		}
	}
	return len(source) - 1
}

func hasIdentifierAt(source string, index int, identifier string) bool {
	if index+len(identifier) > len(source) || source[index:index+len(identifier)] != identifier {
		return false
	}
	if index > 0 && (javascriptIdentifierChar(source[index-1]) || source[index-1] == '.') {
		return false
	}
	return index+len(identifier) == len(source) || !javascriptIdentifierChar(source[index+len(identifier)])
}

func identifierAt(source string, index int) string {
	if index >= len(source) || !javascriptIdentifierStart(source[index]) {
		return ""
	}
	if index > 0 && (javascriptIdentifierChar(source[index-1]) || source[index-1] == '.') {
		return ""
	}
	end := index + 1
	for end < len(source) && javascriptIdentifierChar(source[end]) {
		end++
	}
	return source[index:end]
}

func linePrefixIsWhitespace(source string, index int) bool {
	lineStart := strings.LastIndex(source[:index], "\n") + 1
	return strings.TrimSpace(source[lineStart:index]) == ""
}

func javascriptStringAt(source string, start int) (string, int, bool) {
	if start >= len(source) || source[start] != '\'' && source[start] != '"' {
		return "", start, false
	}
	quote := source[start]
	for index := start + 1; index < len(source); index++ {
		if source[index] == '\\' {
			return "", start, false
		}
		if source[index] == quote {
			return source[start+1 : index], index, true
		}
	}
	return "", start, false
}

func validJavaScriptIdentifier(value string) bool {
	if value == "" || !javascriptIdentifierStart(value[0]) {
		return false
	}
	for index := 1; index < len(value); index++ {
		if !javascriptIdentifierChar(value[index]) {
			return false
		}
	}
	return true
}

func indexOfIdentifier(source, identifier string) int {
	for index := 0; index < len(source); index++ {
		if hasIdentifierAt(source, index, identifier) {
			return index
		}
	}
	return -1
}

func skipSpaces(source string, index int) int {
	for index < len(source) && (source[index] == ' ' || source[index] == '\t' || source[index] == '\n' || source[index] == '\r') {
		index++
	}
	return index
}

func javascriptIdentifierChar(value byte) bool {
	return javascriptIdentifierStart(value) || value >= '0' && value <= '9'
}

func javascriptIdentifierStart(value byte) bool {
	return value == '_' || value == '$' || value >= 'a' && value <= 'z' || value >= 'A' && value <= 'Z'
}

func containsExactID(source, id string) bool {
	start := 0
	for {
		index := strings.Index(source[start:], id)
		if index < 0 {
			return false
		}
		index += start
		beforeOK := index == 0 || !scenarioIDChar(source[index-1])
		after := index + len(id)
		afterOK := after == len(source) || !scenarioIDChar(source[after])
		if beforeOK && afterOK {
			return true
		}
		start = index + 1
	}
}

func containsAllExactIDs(source string, ids []string) bool {
	for _, id := range ids {
		if !containsExactID(source, id) {
			return false
		}
	}
	return true
}

func scenarioIDChar(value byte) bool {
	return javascriptIdentifierChar(value) || value == '-'
}

func markdownHeadingContainsID(data []byte, id string) bool {
	for _, line := range strings.Split(string(data), "\n") {
		hashCount := 0
		for hashCount < len(line) && line[hashCount] == '#' {
			hashCount++
		}
		if hashCount == 0 || hashCount > 6 || hashCount == len(line) || (line[hashCount] != ' ' && line[hashCount] != '\t') {
			continue
		}
		heading := line[hashCount:]
		if containsExactID(heading, id) {
			return true
		}
	}
	return false
}

func hasPathReference(refs []string) bool {
	for _, ref := range refs {
		if strings.HasPrefix(ref, "PATH-") {
			return true
		}
	}
	return false
}

func countCases(model ScenarioModel, cases []Case) Counts {
	counts := Counts{Facts: len(model.Facts), Rules: len(model.Rules), Cases: len(cases)}
	for _, currentCase := range cases {
		if currentCase.Polarity == "positive" {
			counts.Positive++
		} else {
			counts.Negative++
		}
		if currentCase.Required {
			counts.RequiredBranches++
		}
		if currentCase.BrowserRequired {
			counts.BrowserRequiredCases++
		}
	}
	counts.Branches = len(cases)
	return counts
}

func branchCoverage(cases []Case) BranchCoverage {
	coverage := BranchCoverage{}
	for _, currentCase := range cases {
		if !currentCase.Required {
			continue
		}
		coverage.Required++
		coverage.Covered++
		if currentCase.Polarity == "positive" {
			coverage.AllowRequired++
			coverage.AllowCovered++
		} else {
			coverage.RejectRequired++
			coverage.RejectCovered++
		}
	}
	coverage.Percentage = percentage(coverage.Covered, coverage.Required)
	return coverage
}

func ratioFor(counts Counts, profile string) Ratio {
	return Ratio{
		Positive: counts.Positive, Negative: counts.Negative,
		NegativeToPositive:        floatRatio(counts.Negative, counts.Positive),
		MinimumNegativeToPositive: minimumNegativeRatio(profile),
	}
}

func minimumNegativeRatio(profile string) float64 {
	switch profile {
	case "critical":
		return 3
	case "rule-dense":
		return 2
	default:
		return 1
	}
}

func validateModuleName(module string) error {
	if !moduleNamePattern.MatchString(module) || strings.Contains(module, "/") || strings.Contains(module, `\`) || module == "." || module == ".." {
		return fmt.Errorf("invalid module name %q: expected lowercase-kebab without path traversal", module)
	}
	return nil
}

func securePrototypesRoot(root string) (string, string, error) {
	base := filepath.Join(root, prototypeRoot)
	info, err := os.Lstat(base)
	if err != nil {
		return "", "", fmt.Errorf("inspect prototypes root: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return "", "", fmt.Errorf("prototypes root is a symlink")
	}
	if !info.IsDir() {
		return "", "", fmt.Errorf("prototypes root is not a directory")
	}
	realBase, err := filepath.EvalSymlinks(base)
	if err != nil {
		return "", "", fmt.Errorf("resolve prototypes root: %w", err)
	}
	return base, realBase, nil
}

func secureModuleDirectory(root, module string) (string, string, error) {
	base, realBase, err := securePrototypesRoot(root)
	if err != nil {
		return "", "", err
	}
	directory := filepath.Join(base, module)
	if filepath.Base(directory) != module {
		return "", "", fmt.Errorf("module directory does not match module %q", module)
	}
	info, err := os.Lstat(directory)
	if err != nil {
		return "", "", fmt.Errorf("inspect module directory: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return "", "", fmt.Errorf("module directory %s is a symlink", module)
	}
	if !info.IsDir() {
		return "", "", fmt.Errorf("module path %s is not a directory", module)
	}
	realDirectory, err := filepath.EvalSymlinks(directory)
	if err != nil {
		return "", "", fmt.Errorf("resolve module directory: %w", err)
	}
	if !pathWithin(realBase, realDirectory) {
		return "", "", fmt.Errorf("resolved module directory escapes prototypes root")
	}
	return directory, realDirectory, nil
}

func pathWithin(root, candidate string) bool {
	relative, err := filepath.Rel(root, candidate)
	if err != nil {
		return false
	}
	return relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && relative != ""
}

func validProfile(profile string) bool {
	return profile == "ordinary" || profile == "rule-dense" || profile == "critical"
}

func registerID(registry map[string]string, id, kind string) error {
	if id == "" {
		return fmt.Errorf("%s id must not be empty", kind)
	}
	if previous, exists := registry[id]; exists {
		return fmt.Errorf("duplicate id %q: already used by %s and %s", id, previous, kind)
	}
	registry[id] = kind
	return nil
}

func readRequiredFile(directory, name string) ([]byte, error) {
	path := filepath.Join(directory, name)
	info, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("refuse symlink source file %s", path)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("source file %s is not regular", path)
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	data, readErr := io.ReadAll(file)
	closeErr := file.Close()
	if readErr != nil {
		return nil, fmt.Errorf("read %s: %w", path, readErr)
	}
	if closeErr != nil {
		return nil, fmt.Errorf("close %s: %w", path, closeErr)
	}
	postInfo, postErr := os.Lstat(path)
	if postErr != nil {
		return nil, fmt.Errorf("recheck %s: %w", path, postErr)
	}
	if postInfo.Mode()&os.ModeSymlink != 0 || !postInfo.Mode().IsRegular() {
		return nil, fmt.Errorf("source file %s changed to non-regular path", path)
	}
	return data, nil
}

func validateSchema(name string, data []byte) error {
	return schema.NewEmbeddedValidator().ValidateBytes(name, data)
}

func decodeStrict(data []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	decoder.UseNumber()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return fmt.Errorf("trailing JSON value")
		}
		return fmt.Errorf("trailing data: %w", err)
	}
	return nil
}

func marshalStable(value any) ([]byte, error) {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

func fingerprint(parts ...[]byte) string {
	input := make([]byte, 0)
	for _, part := range parts {
		input = append(input, 0)
		input = append(input, part...)
	}
	sum := sha256.Sum256(input)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func cloneStrings(values []string) []string {
	return append([]string(nil), values...)
}

func cloneWitness(values map[string]string) map[string]string {
	copyValues := make(map[string]string, len(values))
	for key, value := range values {
		copyValues[key] = value
	}
	return copyValues
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func hasEmpty(values []string) bool {
	for _, value := range values {
		if strings.TrimSpace(value) == "" {
			return true
		}
	}
	return false
}

func percentage(numerator, denominator int) float64 {
	if denominator == 0 {
		return 100
	}
	return float64(numerator) / float64(denominator) * 100
}

func floatRatio(numerator, denominator int) float64 {
	if denominator == 0 {
		return 0
	}
	return float64(numerator) / float64(denominator)
}

func requiredCasesCount(values map[string]struct{}) int {
	return len(values)
}
