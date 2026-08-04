package e2ecoverage

import (
	"fmt"
	"io"
	"math"
	"sort"
	"strings"
)

// LevelOrder maps fidelity labels to sortable ranks.
var LevelOrder = map[string]int{
	"L0": 0,
	"L1": 1,
	"L2": 2,
	"L3": 3,
	"L4": 4,
}

// Report holds aggregate E2E coverage scores for a scenario inventory.
type Report struct {
	InventoryPath string
	InventorySHA  string

	IDPresenceNumer  int
	IDPresenceDenom  int
	IDPresence       float64
	FidelityScore    float64
	HookSurfaceNum   int
	HookSurfaceDenom int
	HookSurface      string
	OrganicSpine     int
	BelowL3          []Scenario
	GateFailures     []string
	GatePassed       bool
}

const (
	setCT          = "CT"
	setAC          = "AC"
	setHook        = "HOOK"
	setTaskClosing = "TASK-CLOSING"
	spineID        = "SPINE-S2-S11"
)

// Score computes coverage aggregates from an inventory document.
func Score(inv Inventory) Report {
	report := Report{
		IDPresenceDenom:  0,
		HookSurfaceDenom: 0,
	}

	var fidelitySum float64
	ctacCount := 0

	for _, sc := range inv.Scenarios {
		switch sc.Set {
		case setCT, setAC:
			ctacCount++
			report.IDPresenceDenom++
			if len(sc.TestRefs) > 0 {
				report.IDPresenceNumer++
			}
			fidelitySum += weight(inv.Weights, sc.Fidelity)
			if levelRank(sc.Fidelity) < levelRank("L3") {
				report.BelowL3 = append(report.BelowL3, sc)
			}
		case setHook:
			report.HookSurfaceDenom++
			if levelRank(sc.Fidelity) >= levelRank("L3") {
				report.HookSurfaceNum++
			}
		case setTaskClosing:
			if sc.ID == spineID {
				if sc.Fidelity == "L4" {
					report.OrganicSpine = 1
				}
			}
		}
	}

	if report.IDPresenceDenom > 0 {
		report.IDPresence = float64(report.IDPresenceNumer) / float64(report.IDPresenceDenom)
	}
	if ctacCount > 0 {
		report.FidelityScore = fidelitySum / float64(ctacCount)
	}
	report.HookSurface = fmt.Sprintf("%d/%d", report.HookSurfaceNum, report.HookSurfaceDenom)

	sort.Slice(report.BelowL3, func(i, j int) bool {
		return report.BelowL3[i].ID < report.BelowL3[j].ID
	})

	report.GateFailures = evaluateGate(report)
	report.GatePassed = len(report.GateFailures) == 0
	return report
}

func evaluateGate(r Report) []string {
	var failures []string
	if r.IDPresence < 1.0 {
		failures = append(failures, fmt.Sprintf("ID_Presence %.3f < 1.0", r.IDPresence))
	}
	// Round to milli-precision so decimal 0.85 from L3/L4 weights is not
	// rejected by IEEE float noise (16*0.7/32 prints as 0.850 but is < 0.85).
	if math.Round(r.FidelityScore*1000)/1000 < 0.85 {
		failures = append(failures, fmt.Sprintf("FidelityScore %.3f < 0.85", r.FidelityScore))
	}
	for _, sc := range r.BelowL3 {
		failures = append(failures, fmt.Sprintf("%s fidelity %s < L3", sc.ID, sc.Fidelity))
	}
	if r.OrganicSpine != 1 {
		failures = append(failures, "OrganicSpine != 1")
	}
	if r.HookSurfaceNum < 5 {
		failures = append(failures, fmt.Sprintf("HookSurface %s < 5/7", r.HookSurface))
	}
	return failures
}

func weight(weights map[string]float64, level string) float64 {
	if w, ok := weights[level]; ok {
		return w
	}
	return 0
}

func levelRank(level string) int {
	if r, ok := LevelOrder[level]; ok {
		return r
	}
	return -1
}

// FormatReport renders a human-readable score report.
func FormatReport(w io.Writer, r Report) {
	fmt.Fprintf(w, "E2E Coverage (%s)\n", "REQ-039")
	if r.InventoryPath != "" {
		fmt.Fprintf(w, "Inventory: %s\n", r.InventoryPath)
	}
	if r.InventorySHA != "" {
		fmt.Fprintf(w, "Inventory SHA-256: %s\n", r.InventorySHA)
	}
	fmt.Fprintln(w)
	fmt.Fprintf(w, "ID_Presence (CT∪AC): %.3f (%d/%d)\n", r.IDPresence, r.IDPresenceNumer, r.IDPresenceDenom)
	fmt.Fprintf(w, "FidelityScore (CT∪AC): %.3f\n", r.FidelityScore)
	fmt.Fprintf(w, "HookSurface (L3+): %s\n", r.HookSurface)
	fmt.Fprintf(w, "OrganicSpine: %d\n", r.OrganicSpine)
	fmt.Fprintln(w)

	if len(r.BelowL3) > 0 {
		fmt.Fprintln(w, "Below L3 (CT/AC):")
		for _, sc := range r.BelowL3 {
			fmt.Fprintf(w, "  %s %s\n", sc.ID, sc.Fidelity)
		}
		fmt.Fprintln(w)
	}

	if r.GatePassed {
		fmt.Fprintln(w, "E2E ready gate: PASS")
		return
	}
	fmt.Fprintln(w, "E2E ready gate: FAIL")
	for _, f := range r.GateFailures {
		fmt.Fprintf(w, "  - %s\n", f)
	}
}

// FormatReportOneLine returns a compact summary for embedding in delivery docs.
func FormatReportOneLine(r Report) string {
	var b strings.Builder
	fmt.Fprintf(&b, "ID_Presence=%.3f FidelityScore=%.3f HookSurface=%s OrganicSpine=%d",
		r.IDPresence, r.FidelityScore, r.HookSurface, r.OrganicSpine)
	return b.String()
}
