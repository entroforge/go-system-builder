package scenario

// ScenarioModel is the module-owned source of truth for facts and rule
// branches. It intentionally contains no requirement or iteration ownership.
type ScenarioModel struct {
	Module          string `json:"module"`
	CoverageProfile string `json:"coverage_profile"`
	Facts           []Fact `json:"facts"`
	Rules           []Rule `json:"rules"`
}

type Fact struct {
	ID         string      `json:"id"`
	Partitions []Partition `json:"partitions"`
}

type Partition struct {
	ID    string `json:"id"`
	Value any    `json:"value"`
}

type Rule struct {
	ID         string   `json:"id"`
	SourceRefs []string `json:"source_refs"`
	Risk       string   `json:"risk"`
	Branches   []Branch `json:"branches"`
}

type Branch struct {
	ID              string            `json:"id"`
	CaseID          string            `json:"case_id"`
	Title           string            `json:"title"`
	Polarity        string            `json:"polarity"`
	Required        bool              `json:"required"`
	Witness         map[string]string `json:"witness"`
	Oracle          Oracle            `json:"oracle"`
	FixtureID       string            `json:"fixture_id"`
	StoryRefs       []string          `json:"story_refs"`
	FlowRefs        []string          `json:"flow_refs"`
	BrowserRequired bool              `json:"browser_required"`
}

type Oracle struct {
	Visible              []string `json:"visible,omitempty"`
	TerminalState        string   `json:"terminal_state,omitempty"`
	PersistedEffects     []string `json:"persisted_effects,omitempty"`
	ForbiddenSideEffects []string `json:"forbidden_side_effects,omitempty"`
	Rejection            string   `json:"rejection,omitempty"`
	ExpectedState        string   `json:"expected_state,omitempty"`
	Recovery             string   `json:"recovery,omitempty"`
	RecoverySourceRefs   []string `json:"recovery_source_refs,omitempty"`
	RecoveryReason       string   `json:"recovery_reason,omitempty"`
}

type FixtureContract struct {
	Module   string    `json:"module"`
	Fixtures []Fixture `json:"fixtures"`
}

type Fixture struct {
	ID        string   `json:"id"`
	Persona   string   `json:"persona"`
	Synthetic bool     `json:"synthetic"`
	Setup     []string `json:"setup"`
	Cleanup   []string `json:"cleanup"`
}

type Case struct {
	ID              string            `json:"id"`
	RuleID          string            `json:"rule_id"`
	BranchID        string            `json:"branch_id"`
	Title           string            `json:"title"`
	Polarity        string            `json:"polarity"`
	Required        bool              `json:"required"`
	Witness         map[string]string `json:"witness"`
	Oracle          Oracle            `json:"oracle"`
	FixtureID       string            `json:"fixture_id"`
	StoryRefs       []string          `json:"story_refs"`
	FlowRefs        []string          `json:"flow_refs"`
	BrowserRequired bool              `json:"browser_required"`
}

type CasesOutput struct {
	Module string `json:"module"`
	Cases  []Case `json:"cases"`
}

type CoverageOutput struct {
	Module                 string         `json:"module"`
	CoverageProfile        string         `json:"coverage_profile"`
	Counts                 Counts         `json:"counts"`
	RequiredBranchCoverage BranchCoverage `json:"required_branch_coverage"`
	Ratio                  Ratio          `json:"ratio"`
}

type Counts struct {
	Facts                int `json:"facts"`
	Rules                int `json:"rules"`
	Branches             int `json:"branches"`
	Cases                int `json:"cases"`
	Positive             int `json:"positive"`
	Negative             int `json:"negative"`
	RequiredBranches     int `json:"required_branches"`
	BrowserRequiredCases int `json:"browser_required_cases"`
}

type BranchCoverage struct {
	Required       int     `json:"required"`
	Covered        int     `json:"covered"`
	Percentage     float64 `json:"percentage"`
	AllowRequired  int     `json:"allow_required"`
	AllowCovered   int     `json:"allow_covered"`
	RejectRequired int     `json:"reject_required"`
	RejectCovered  int     `json:"reject_covered"`
}

type Ratio struct {
	Positive                  int     `json:"positive"`
	Negative                  int     `json:"negative"`
	NegativeToPositive        float64 `json:"negative_to_positive"`
	MinimumNegativeToPositive float64 `json:"minimum_negative_to_positive"`
}

type BrowserSpecCoverage struct {
	RequiredCases  int     `json:"required_cases"`
	CoveredCases   int     `json:"covered_cases"`
	CasePercentage float64 `json:"case_percentage"`
	RequiredPaths  int     `json:"required_paths"`
	CoveredPaths   int     `json:"covered_paths"`
	PathPercentage float64 `json:"path_percentage"`
}

type OutputFingerprints struct {
	Cases    string `json:"cases"`
	Coverage string `json:"coverage"`
}

// Report is returned by all public scenario operations and is safe to encode
// directly as JSON for QA and evidence records.
type Report struct {
	Module              string              `json:"module"`
	Counts              Counts              `json:"counts"`
	Coverage            BranchCoverage      `json:"coverage"`
	Ratio               Ratio               `json:"ratio"`
	BrowserSpecCoverage BrowserSpecCoverage `json:"browser_spec_coverage"`
	InputFingerprint    string              `json:"input_fingerprint"`
	OutputFingerprints  OutputFingerprints  `json:"output_fingerprints"`
}

type ValidateOptions struct {
	RequireSpecs bool
	// AutoSpecs enforces browser-spec coverage for modules whose Playwright
	// spec tree already exists (specs are S6+ artifacts — absent trees are
	// expected before building and are not failures).
	AutoSpecs bool
}
