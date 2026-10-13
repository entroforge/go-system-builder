package e2erun

import (
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/entroforge/go-system-builder/internal/fileview"
	"github.com/entroforge/go-system-builder/internal/schema"
)

// Requirement is one explicitly required business oracle/environment tuple.
// It lives in ReviewPlan, not a separate per-REQ registry. Claim ownership is
// checked by the existing Plan assignment partition.
type Requirement struct {
	ClaimID string  `json:"claim_id"`
	CaseRef CaseRef `json:"case_ref"`
	Project string  `json:"project"`
	Browser string  `json:"browser"`
}

type Oracle struct {
	ModuleRef      string `json:"module_ref"`
	CaseID         string `json:"case_id"`
	OracleRef      string `json:"oracle_ref"`
	Value          string `json:"value"`
	DocumentSHA256 string `json:"document_sha256"`
}

var modulePath = regexp.MustCompile(`^docs/design/prototypes/([a-z0-9]+(?:-[a-z0-9]+)*)/cases\.json$`)

// ReadOracles derives the finite business denominator from the authoritative
// cases.json files. Only the caller's formally bound modules may enter. No
// browser/persona/profile Cartesian product is invented by this function.
func ReadOracles(files fileview.Reader, modules []string) ([]Oracle, error) {
	seen := map[string]bool{}
	out := []Oracle{}
	for _, module := range modules {
		match := modulePath.FindStringSubmatch(module)
		if len(match) != 2 || seen[module] {
			return nil, fmt.Errorf("invalid/duplicate module_ref %q", module)
		}
		seen[module] = true
		data, err := files.ReadFile(module)
		if err != nil {
			return nil, err
		}
		if err := schema.NewEmbeddedValidator().ValidateBytes("scenario-cases.schema.json", data); err != nil {
			return nil, fmt.Errorf("CASE authority %s: %w", module, err)
		}
		var doc struct {
			Module string `json:"module"`
			Cases  []struct {
				ID              string         `json:"id"`
				Required        bool           `json:"required"`
				BrowserRequired bool           `json:"browser_required"`
				Oracle          map[string]any `json:"oracle"`
			} `json:"cases"`
		}
		if err := json.Unmarshal(data, &doc); err != nil {
			return nil, err
		}
		if doc.Module != match[1] {
			return nil, fmt.Errorf("CASE module identity mismatch: %s declares %s", module, doc.Module)
		}
		ids := map[string]bool{}
		for _, c := range doc.Cases {
			if ids[c.ID] {
				return nil, fmt.Errorf("duplicate CASE %s in %s", c.ID, module)
			}
			ids[c.ID] = true
			if !c.Required || !c.BrowserRequired {
				continue
			}
			for _, key := range []string{"visible", "terminal_state", "persisted_effects", "forbidden_side_effects", "rejection", "expected_state", "recovery"} {
				prefix := "/oracle/" + key
				add := func(ref, value string) {
					out = append(out, Oracle{ModuleRef: module, CaseID: c.ID, OracleRef: ref, Value: value, DocumentSHA256: digest(data)})
				}
				switch value := c.Oracle[key].(type) {
				case string:
					add(prefix, value)
				case []any:
					for i, item := range value {
						add(prefix+"/"+strconv.Itoa(i), item.(string))
					}
				}
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return oracleKey(out[i].ModuleRef, out[i].CaseID, out[i].OracleRef) < oracleKey(out[j].ModuleRef, out[j].CaseID, out[j].OracleRef)
	})
	return out, nil
}

func oracleKey(module, caseID, oracle string) string {
	data, _ := json.Marshal([]string{module, caseID, oracle})
	return string(data)
}

func RequirementKey(r Requirement) string {
	data, _ := json.Marshal(struct {
		CaseRef CaseRef `json:"case_ref"`
		Project string  `json:"project"`
		Browser string  `json:"browser"`
	}{r.CaseRef, r.Project, r.Browser})
	return string(data)
}

// ValidateRequirements rejects a missing oracle even when test totals match.
// The dimensions remain explicit Planner assertions, subject to independent
// contract review; a count or a CASE substring cannot create an obligation.
func ValidateRequirements(required []Requirement, oracles []Oracle) error {
	denominator, covered, seen := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, oracle := range oracles {
		denominator[oracleKey(oracle.ModuleRef, oracle.CaseID, oracle.OracleRef)] = true
	}
	for _, r := range required {
		ref := r.CaseRef
		if r.ClaimID == "" || strings.TrimSpace(ref.Persona) == "" || strings.TrimSpace(ref.DataProfile) == "" {
			return fmt.Errorf("every E2E requirement needs a claim, explicit persona and data_profile")
		}
		if r.Browser != "chromium" && r.Browser != "firefox" && r.Browser != "webkit" {
			return fmt.Errorf("unsupported required browser %q", r.Browser)
		}
		key := oracleKey(ref.ModuleRef, ref.CaseID, ref.OracleRef)
		if !denominator[key] {
			return fmt.Errorf("requirement is outside the formal CASE/oracle denominator: %s", key)
		}
		identity := RequirementKey(r)
		if seen[identity] {
			return fmt.Errorf("duplicate E2E requirement/owner: %s", identity)
		}
		seen[identity], covered[key] = true, true
	}
	var missing []string
	for key := range denominator {
		if !covered[key] {
			missing = append(missing, key)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		return fmt.Errorf("required CASE/oracles lack explicit environment/owner coverage: %s", strings.Join(missing, ", "))
	}
	return nil
}

// ValidateExecutionCoverage consumes already-authenticated indexed receipts.
// It does not authenticate caller JSON. The Runtime producer/consumer must
// establish receipt kind, producer, immutable bytes and current authority first.
// One actual test may answer several explicitly annotated oracles; its attempt
// ID is retained once, never relabeled as independent execution per oracle.
func ValidateExecutionCoverage(required []Requirement, receipts []Receipt) error {
	if len(required) == 0 || len(receipts) == 0 {
		return fmt.Errorf("execution coverage requires a nonempty explicit denominator and observations")
	}
	wanted := map[string]bool{}
	covered := map[string]string{}
	runs := map[string]bool{}
	executions := map[string]bool{}
	var context *Binding
	var environment *Receipt
	for _, r := range required {
		key := RequirementKey(r)
		if wanted[key] {
			return fmt.Errorf("duplicate required execution tuple %s", key)
		}
		wanted[key] = true
	}
	for _, receipt := range receipts {
		if context == nil {
			binding := receipt.Binding
			context = &binding
			environment = &receipt
		} else if receipt.Binding.RuntimeID != context.RuntimeID || receipt.Binding.Generation != context.Generation || receipt.Binding.Round != context.Round || receipt.Binding.SourceCommit != context.SourceCommit || receipt.Binding.SubjectDigest != context.SubjectDigest {
			return fmt.Errorf("receipts from different Runtime/round/candidate inputs cannot be merged")
		}
		if receipt.ProfileSHA256 != environment.ProfileSHA256 || receipt.InputSHA256 != environment.InputSHA256 || receipt.Platform != environment.Platform || !reflect.DeepEqual(receipt.Tools, environment.Tools) {
			return fmt.Errorf("receipts with different profile/input/toolchain/platform snapshots cannot be merged")
		}
		if receipt.Mode != "execution" || !receipt.Complete || receipt.Outcome != "pass" || receipt.ExitCode != 0 {
			return fmt.Errorf("receipt %s is not a complete passing execution", receipt.RunID)
		}
		if receipt.RunID == "" || runs[receipt.RunID] {
			return fmt.Errorf("missing/duplicate run ID %q", receipt.RunID)
		}
		runs[receipt.RunID] = true
		byID := map[string]Test{}
		for _, test := range receipt.Tests {
			if _, dup := byID[test.ID]; dup {
				return fmt.Errorf("duplicate test ID %s", test.ID)
			}
			byID[test.ID] = test
		}
		for _, attempt := range receipt.Attempts {
			if !attempt.BrowserPageObserved {
				return fmt.Errorf("attempt %s has no observed browser page; a passing test function is insufficient for browser coverage", attempt.ExecutionID)
			}
			if attempt.ExecutionID != receipt.RunID+":"+attempt.TestID+":0" || executions[attempt.ExecutionID] || attempt.Retry != 0 || attempt.Status != "passed" {
				return fmt.Errorf("duplicate, retried or nonpassing attempt %s", attempt.ExecutionID)
			}
			executions[attempt.ExecutionID] = true
			test, ok := byID[attempt.TestID]
			if !ok || test.ExpectedStatus != "passed" {
				return fmt.Errorf("attempt %s has no eligible collected test", attempt.ExecutionID)
			}
			for _, ref := range test.CaseRefs {
				key := RequirementKey(Requirement{CaseRef: ref, Project: test.Project, Browser: test.Browser})
				if !wanted[key] {
					continue
				} // Shared observations may answer another Claim too.
				if previous := covered[key]; previous != "" && previous != attempt.ExecutionID {
					return fmt.Errorf("overlapping execution coverage for %s: %s and %s", key, previous, attempt.ExecutionID)
				}
				covered[key] = attempt.ExecutionID
			}
		}
	}
	var missing []string
	for key := range wanted {
		if covered[key] == "" {
			missing = append(missing, key)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		return fmt.Errorf("required execution tuples missing: %s", strings.Join(missing, ", "))
	}
	return nil
}
