package workspace

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/entroforge/go-system-builder/internal/checkexec"
)

func (b *ExecutionRegistry) BeginCheckRun(ctx context.Context, e Execution, index int) (*checkexec.RunAttempt, error) {
	if index < 0 || index >= len(e.Checks) {
		return nil, fmt.Errorf("select a declared check index")
	}
	binding := CheckBinding(e)
	return checkexec.BeginInvocation(ctx, filepath.Join(b.MainRoot, ".claude/evidence/check-runs"), binding, index, "workspace")
}

func (b *ExecutionRegistry) CheckRuns(e Execution) ([]checkexec.RunRecord, error) {
	dir := filepath.Join(b.MainRoot, ".claude/evidence/check-runs")
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return []checkexec.RunRecord{}, nil
	}
	if err != nil {
		return nil, err
	}
	rows := []checkexec.RunRecord{}
	// Diagnostics are bounded; overflow tells the owner to inspect evidence.
	if len(entries) > 4096 {
		return nil, fmt.Errorf("check running diagnostics exceed bounded observation")
	}
	for _, entry := range entries {
		if !entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		r, found, err := checkexec.ReadRunRecord(filepath.Join(dir, entry.Name(), "run.json"))
		if err != nil {
			continue
		}
		if found && r.Binding == CheckBinding(e) {
			rows = append(rows, r)
		}
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].UpdatedAt > rows[j].UpdatedAt })
	if len(rows) > 64 {
		rows = rows[:64]
	}
	return rows, nil
}
