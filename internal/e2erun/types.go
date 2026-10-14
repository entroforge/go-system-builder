// Package e2erun owns the controlled Playwright invocation. Source mentions
// and caller-authored receipts cannot construct an Observation. The Runtime
// producer must separately bind the observation to its current authority/CAS.
package e2erun

import "encoding/json"

type CaseRef struct {
	ModuleRef   string `json:"module_ref"`
	CaseID      string `json:"case_id"`
	OracleRef   string `json:"oracle_ref"`
	Persona     string `json:"persona"`
	DataProfile string `json:"data_profile"`
}

type FileDigest struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

// Tool points at a locally provisioned tool, outside the product tree. Run
// copies and hashes it before execution; no install, npx or download occurs.
type Tool struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

type Platform struct {
	OS            string `json:"os"`
	Architecture  string `json:"architecture"`
	KernelRelease string `json:"kernel_release"`
}

type Profile struct {
	SchemaVersion  string   `json:"schema_version"`
	Platform       Platform `json:"platform"`
	Config         string   `json:"config"`
	SourceRoots    []string `json:"source_roots"`
	Node           Tool     `json:"node"`
	Modules        Tool     `json:"modules"`
	Browsers       Tool     `json:"browsers"`
	System         Tool     `json:"system"`
	Isolator       Tool     `json:"isolator"`
	TimeoutSeconds int      `json:"timeout_seconds"`
}

type Binding struct {
	RuntimeID     string `json:"runtime_id"`
	Generation    int    `json:"baseline_generation"`
	Round         int    `json:"review_round"`
	SourceCommit  string `json:"source_commit"`
	AssignmentID  string `json:"assignment_id,omitempty"`
	SubjectDigest string `json:"subject_digest"`
}

type Test struct {
	ID             string    `json:"id"`
	Path           string    `json:"path"`
	Project        string    `json:"project"`
	SelectionPath  string    `json:"selection_path"`
	Browser        string    `json:"browser"`
	TitlePath      []string  `json:"title_path"`
	CaseRefs       []CaseRef `json:"case_refs"`
	ExpectedStatus string    `json:"expected_status"`
}

type Attempt struct {
	ExecutionID         string   `json:"execution_id"`
	TestID              string   `json:"test_id"`
	Retry               int      `json:"retry"`
	Status              string   `json:"status"`
	DurationMS          float64  `json:"duration_ms"`
	Errors              []string `json:"errors"`
	BrowserPageObserved bool     `json:"browser_page_observed"`
	Steps               []Step   `json:"steps"`
}

// Step is an adapter-observed Playwright API event. It is not an assertion
// adequacy score. The current adapter admits browser coverage only when the
// test's own attempt created a real page; shared pages created outside that
// attempt require a future adapter with an explicit ownership model.
type Step struct {
	Category   string  `json:"category"`
	Title      string  `json:"title"`
	DurationMS float64 `json:"duration_ms"`
	Error      string  `json:"error,omitempty"`
}

// Receipt records invocation, not the sufficiency of the business assertions.
// Collection/execution admission must still validate exact required sets,
// module-qualified CASE/oracles, assignment ownership and current input hashes.
type Receipt struct {
	Version          string            `json:"schema_version"`
	Platform         Platform          `json:"platform"`
	RunID            string            `json:"run_id"`
	Mode             string            `json:"mode"`
	Binding          Binding           `json:"binding"`
	ProfileSHA256    string            `json:"profile_sha256"`
	ProfileRef       *FileDigest       `json:"profile_ref,omitempty"`
	InputSHA256      string            `json:"input_sha256"`
	Inputs           []FileDigest      `json:"inputs"`
	Tools            map[string]string `json:"tools"`
	CollectionSHA256 string            `json:"collection_sha256,omitempty"`
	Command          []string          `json:"command"`
	SelectedTestIDs  []string          `json:"selected_test_ids,omitempty"`
	AttemptedAt      string            `json:"attempted_at"`
	Stage            string            `json:"stage"`
	Invoked          bool              `json:"invoked"`
	StartedAt        string            `json:"started_at,omitempty"`
	PreparationMS    int64             `json:"preparation_ms"`
	ElapsedMS        int64             `json:"elapsed_ms"`
	ExitCode         int               `json:"exit_code"`
	Complete         bool              `json:"complete"`
	Outcome          string            `json:"outcome"`
	Tests            []Test            `json:"tests"`
	Attempts         []Attempt         `json:"attempts"`
	RawEvents        string            `json:"raw_events"`
	Output           string            `json:"output"`
	Problems         []string          `json:"problems"`
}

// Observation has no exported state or JSON decoder. Only the actual runner
// produces its immutable serialized receipt. A zero value is never admitted.
type Observation struct{ data []byte }

func (o *Observation) Bytes() []byte {
	if o == nil {
		return nil
	}
	return append([]byte(nil), o.data...)
}

func (o *Observation) Receipt() (Receipt, bool) {
	var r Receipt
	if o == nil || len(o.data) == 0 {
		return r, false
	}
	err := json.Unmarshal(o.data, &r)
	return r, err == nil
}
