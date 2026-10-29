package designfoundation

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHandoffBudgetMeasuresDeclaredSurfaceAndReportsOmissions(t *testing.T) {
	for _, tc := range []struct {
		name, selected, reference, first, second, inline, otherInline, omission string
		oversize, noCard, noPack, noProfile, unnumbered                         bool
	}{
		{name: "unrelated SUR-01 is not the packet", selected: "surface-profiles/operations.md@v1.0.0", reference: "SUR-17@v1.0.0", first: strings.Repeat("unrelated\n", 150), second: "small current profile\n"},
		{name: "actual SUR-17 exceeds target", selected: "surface-profiles/operations.md@v1.0.0", reference: "SUR-17@v1.0.0", first: "small unrelated\n", second: strings.Repeat("current\n", 150), oversize: true},
		{name: "large inline row counted", selected: "inline", reference: "SUR-17@v1.0.0", inline: strings.Repeat("已声明的当前表面差分", 1500), oversize: true, omission: "inline"},
		{name: "unrelated inline row excluded", selected: "inline", reference: "SUR-17@v1.0.0", otherInline: strings.Repeat("其他表面差分", 2500), omission: "inline"},
		{name: "unnumbered next section is outside card", selected: "surface-profiles/operations.md@v1.0.0", reference: "SUR-17@v1.0.0", otherInline: strings.Repeat("其他表面差分", 2500), second: "small current profile\n", unnumbered: true},
		{name: "unknown surface not guessed", selected: "inline", reference: "SUR-99@v1.0.0", omission: "SUR-99"},
		{name: "missing selected profile", selected: "surface-profiles/operations.md@v1.0.0", reference: "SUR-17@v1.0.0", noProfile: true, omission: "operations.md"},
		{name: "missing card", selected: "inline", reference: "SUR-17@v1.0.0", noCard: true, omission: "§0"},
		{name: "missing derivation", selected: "inline", reference: "SUR-17@v1.0.0", noPack: true, omission: "REQ-731.md"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			kernel := kernelWithVersion("v1.0.0")
			row := "| SUR-17 | operations | " + tc.selected + " | " + tc.inline + " |\n"
			kernel = strings.Replace(kernel, "| SUR-01 | consumer | surface-profiles/consumer.md@v1.0.0 | PROOF-01 |", "| SUR-01 | consumer | surface-profiles/consumer.md@v1.0.0 | PROOF-01 |\n"+row, 1)
			if tc.otherInline != "" {
				kernel = strings.Replace(kernel, "| SUR-01 | consumer | surface-profiles/consumer.md@v1.0.0 | PROOF-01 |", "| SUR-01 | consumer | inline | "+tc.otherInline+" |", 1)
			}
			if tc.noCard {
				kernel = strings.Replace(kernel, "## 0. Next-agent card", "## Card without supported heading", 1)
			}
			if tc.unnumbered {
				kernel = strings.Replace(kernel, "## 8. Surfaces in force", "## Surfaces in force", 1)
			}
			files := map[string]string{
				KernelRel: kernel, TokensJSONRel: "{}",
				"docs/requirements/REQ-731.md":             "# REQ\n> 状态：locked\n> UI impact：changed\n\n| Foundation reference | docs/design/DESIGN.md@v1.0.0 |\n",
				"docs/design/surface-profiles/consumer.md": tc.first,
			}
			if !tc.noProfile {
				files["docs/design/surface-profiles/operations.md"] = tc.second
			}
			if !tc.noPack {
				files["docs/design/derivation/REQ-731.md"] = "# Derivation\n\n> Surface：" + tc.reference + "\n\n<!-- foundation-contract:v1 derivation-active -->\n| ID | 本 REQ 的具体落点 | 需要打开的 GR-* |\n|---|---|---|\n| LAW-01 | current | GR-01 |\n"
			}
			mustWriteTreeGeneric(t, root, files)
			report, err := Check(root)
			if err != nil {
				t.Fatal(err)
			}
			var budgets []Finding
			oversize := false
			for _, f := range report.Findings {
				if strings.HasPrefix(f.Code, "handoff_packet_") {
					budgets = append(budgets, f)
					oversize = oversize || f.Code == "handoff_packet_oversize"
				}
			}
			if oversize != tc.oversize {
				t.Errorf("budget measured the wrong surface or omitted actual bytes: want oversize=%v, got %+v", tc.oversize, budgets)
			}
			if tc.omission != "" {
				found := false
				for _, f := range budgets {
					if f.Code == "handoff_packet_unmeasured" && strings.Contains(f.Detail, tc.omission) {
						found = true
					}
				}
				if !found {
					t.Errorf("partial packet presented as a complete budget: missing %q in %+v", tc.omission, budgets)
				}
			} else {
				found := false
				for _, f := range budgets {
					if f.Code == "handoff_packet_estimate" && strings.Contains(f.Detail, "operations.md") && !strings.Contains(f.Detail, "consumer.md") {
						found = true
					}
				}
				if !found {
					t.Errorf("budget scope does not name the actual declared sources: %+v", budgets)
				}
			}
			for rel, want := range files {
				data, err := os.ReadFile(filepath.Join(root, rel))
				if err != nil || string(data) != want {
					t.Fatalf("budget diagnosis changed source %s: %v", rel, err)
				}
			}
		})
	}
}
