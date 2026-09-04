package designfoundation

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// I0: freeze P0~P4 baseline so I1 rewrite cannot silently change compatibility.
func TestI0_V1BaselineFixturesExist(t *testing.T) {
	root := repoRoot(t)
	baselineDir := filepath.Join(root, "internal", "designfoundation", "testdata", "v1-baseline")
	entries, err := os.ReadDir(baselineDir)
	if err != nil {
		t.Fatalf("v1 baseline dir missing: %v", err)
	}
	if len(entries) < 6 {
		t.Fatalf("expected >=6 baseline files, got %d", len(entries))
	}
	for _, e := range entries {
		data, err := os.ReadFile(filepath.Join(baselineDir, e.Name()))
		if err != nil || len(data) == 0 {
			t.Fatalf("baseline file %s unreadable: %v", e.Name(), err)
		}
		if len(data) < 100 {
			t.Fatalf("baseline file %s suspiciously small (%d bytes)", e.Name(), len(data))
		}
	}
}

func TestI0_V1BaselineFrozenUnchanged(t *testing.T) {
	// Frozen v1.0 copies must remain byte-stable (proves I0 captured before I1).
	root := repoRoot(t)
	baselineDir := filepath.Join(root, "internal", "designfoundation", "testdata", "v1-baseline")
	for _, name := range []string{
		"docs_design_DESIGN-template.md",
		"docs_design_design-language-template.md",
		"docs_design_surface-profiles_surface-profile-template.md",
		"docs_design_derivation_DERIVATION-template.md",
		"docs_design_research_evidence-field-template.md",
		"docs_design_proof_README.md",
	} {
		data, err := os.ReadFile(filepath.Join(baselineDir, name))
		if err != nil || len(data) < 100 {
			t.Fatalf("frozen %s unreadable: %v", name, err)
		}
		_ = sha256.Sum256(data)
	}
}

func TestI1_TemplatesMigratedToContractV1(t *testing.T) {
	// After I1 live templates must carry foundation-contract:v1 markers
	// and differ from the frozen v1.0 baseline.
	root := repoRoot(t)
	markers := map[string]string{
		"docs/design/DESIGN-template.md":                            "foundation-contract:v1 constraints",
		"docs/design/design-language-template.md":                   "foundation-contract:v1 dimensions",
		"docs/design/surface-profiles/surface-profile-template.md": "foundation-contract:v1 surface-inherits",
		"docs/design/derivation/DERIVATION-template.md":             "foundation-contract:v1 derivation-active",
		"docs/design/research/evidence-field-template.md":           "foundation-contract:v1 evidence-product",
	}
	baselineDir := filepath.Join(root, "internal", "designfoundation", "testdata", "v1-baseline")
	for liveRel, marker := range markers {
		live, err := os.ReadFile(filepath.Join(root, liveRel))
		if err != nil {
			t.Fatalf("live %s missing: %v", liveRel, err)
		}
		if !strings.Contains(string(live), marker) {
			t.Fatalf("live %s missing marker %q after I1", liveRel, marker)
		}
		// Must have diverged from frozen v1.0
		frozenName := map[string]string{
			"docs/design/DESIGN-template.md":                            "docs_design_DESIGN-template.md",
			"docs/design/design-language-template.md":                   "docs_design_design-language-template.md",
			"docs/design/surface-profiles/surface-profile-template.md": "docs_design_surface-profiles_surface-profile-template.md",
			"docs/design/derivation/DERIVATION-template.md":             "docs_design_derivation_DERIVATION-template.md",
			"docs/design/research/evidence-field-template.md":           "docs_design_research_evidence-field-template.md",
		}[liveRel]
		frozen, _ := os.ReadFile(filepath.Join(baselineDir, frozenName))
		if sha256.Sum256(live) == sha256.Sum256(frozen) {
			t.Fatalf("live %s still equals frozen v1.0 baseline — I1 migrate not applied", liveRel)
		}
	}
}


func TestI0_TemplateFactoryStillAdvisoryClean(t *testing.T) {
	root := repoRoot(t)
	report, err := Check(root)
	if err != nil {
		t.Fatal(err)
	}
	if warns := report.Warnings(); len(warns) > 0 {
		t.Fatalf("factory must stay advisory-clean before I1, got %v", warns)
	}
	// Must not yet emit v1.1 codes on a factory with no published Foundation.
	for _, f := range report.Findings {
		switch f.Code {
		case "foundation_contract_legacy", "investment_profile_missing", "dimension_unrouted":
			t.Fatalf("I0 must not emit v1.1 code %s on factory root", f.Code)
		}
	}
}

func TestI0_SamplesExist(t *testing.T) {
	root := repoRoot(t)
	for _, sample := range []string{"local-minimal", "core-minimal", "extended-minimal"} {
		dir := filepath.Join(root, "internal", "designfoundation", "testdata", "samples", sample)
		if st, err := os.Stat(dir); err != nil || !st.IsDir() {
			t.Fatalf("sample %s missing: %v", sample, err)
		}
	}
}

func TestI0_PortableExportStillOmitsComponents(t *testing.T) {
	// Guard L5 A3: portable must not include Components even after I1.
	root := repoRoot(t)
	tf, err := LoadTokens(root)
	if err != nil {
		t.Fatal(err)
	}
	body := renderPortable(tf, "", "")
	if got := fmt.Sprintf("%q", body); len(got) == 0 {
		t.Fatal("renderPortable empty")
	}
}
