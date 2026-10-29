package review

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/entroforge/go-system-builder/internal/buildsurface"
)

// BenchmarkCoverageInventoryOriginalSources reads an existing, unmodified S7
// source tree. It never registers the Plan or changes its original evidence.
// Each iteration gets a fresh request cache, as independent hooks do.
func BenchmarkCoverageInventoryOriginalSources(b *testing.B) {
	root := os.Getenv("GSB_REVIEW_BENCHMARK_ROOT")
	planPath := os.Getenv("GSB_REVIEW_BENCHMARK_PLAN")
	if root == "" || planPath == "" {
		b.Skip("set GSB_REVIEW_BENCHMARK_ROOT and GSB_REVIEW_BENCHMARK_PLAN to original S7 sources")
	}
	stateData, err := os.ReadFile(filepath.Join(root, ".claude/loop-state.json"))
	if err != nil {
		b.Fatal(err)
	}
	planData, err := os.ReadFile(planPath)
	if err != nil {
		b.Fatal(err)
	}
	var state map[string]any
	var plan Plan
	if err := json.Unmarshal(stateData, &state); err != nil {
		b.Fatal(err)
	}
	if err := json.Unmarshal(planData, &plan); err != nil {
		b.Fatal(err)
	}
	b.Logf("original state sha256=%s; plan sha256=%s", sha256Of(stateData), sha256Of(planData))
	b.ResetTimer()
	for b.Loop() {
		if err := validateCoverageInventory(root, state, &plan, buildsurface.WithReadCache(context.Background())); err != nil {
			b.Fatal(err)
		}
	}
	b.ReportMetric(float64(len(planData)), "plan_bytes")
	b.ReportMetric(float64(len(plan.Claims)), "claims")
	b.ReportMetric(float64(len(plan.Assignments)), "assignments")
	if plan.ProductBaseline != nil {
		b.ReportMetric(float64(len(plan.ProductBaseline.Contributions)), "contributions")
	}
}
