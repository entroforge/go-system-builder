package review

import (
	"fmt"
	"os"
	"sort"
	"strings"

	loopruntime "github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/semantic"
)

// FrozenSourceDiff is the read-only pre-recovery comparison for a stale S7
// round. It never modifies files; Main performs the actual restoration.
//
//   - Restore lists pinned subjects whose disk bytes drifted and must be
//     restored to the exact pinned bytes before revive.
//   - Missing lists pinned subjects absent from disk that Main must recover.
//   - Preserve lists current files absent from the pinned baseline that must
//     NOT be deleted by a blanket restore (S8 process files, new evidence).
//     They are reported and stay candidates for the unattributed review path.
type FrozenSourceDiff struct {
	Restore  []string `json:"restore"`
	Missing  []string `json:"missing"`
	Preserve []string `json:"preserve"`
}

// DiffFrozenSources compares the pinned ReviewPlan baseline against the
// working tree without modifying anything. Pinned commit SHAs come from the
// plan itself; Main never hand-supplies a hash.
func DiffFrozenSources(root string, plan *Plan, state map[string]any) (*FrozenSourceDiff, error) {
	if plan == nil {
		return nil, fmt.Errorf("frozen source diff requires a plan")
	}
	diff := &FrozenSourceDiff{}
	seen := map[string]bool{}
	for _, subject := range plan.FrozenSubjects {
		path := strings.TrimSpace(subject.Path)
		if path == "" {
			continue
		}
		if seen[path] {
			continue
		}
		seen[path] = true
		diskPath, err := repositoryContainedPath(root, path)
		if err != nil {
			continue
		}
		data, readErr := os.ReadFile(diskPath)
		if readErr != nil {
			if os.IsNotExist(readErr) && !deletionCorroborated(root, state, plan, path, subject.SHA256) {
				diff.Missing = append(diff.Missing, path)
			}
			continue
		}
		if subject.Kind == "deleted" {
			diff.Restore = append(diff.Restore, path)
			continue
		}
		if subject.SHA256 != "" && sha256Of(data) != subject.SHA256 {
			diff.Restore = append(diff.Restore, path)
		}
	}
	drift, err := detectUndeclaredProductDriftWithState(root, plan, state, root)
	if err != nil {
		return nil, fmt.Errorf("frozen source drift scan: %w", err)
	}
	for _, path := range drift {
		if !seen[path] {
			diff.Preserve = append(diff.Preserve, path)
		}
	}
	sort.Strings(diff.Restore)
	sort.Strings(diff.Missing)
	sort.Strings(diff.Preserve)
	return diff, nil
}

// FrozenDiffReport renders the diff as Main-facing recovery guidance.
func FrozenDiffReport(diff *FrozenSourceDiff) string {
	var b strings.Builder
	b.WriteString("Frozen source recovery (read-only comparison; no files changed):\n")
	writeDiffList(&b, "restore to exact pinned bytes", diff.Restore)
	writeDiffList(&b, "missing from disk, recover before revive", diff.Missing)
	writeDiffList(&b, "preserve, never delete with a blanket restore", diff.Preserve)
	if len(diff.Preserve) > 0 {
		b.WriteString("Preserved files stay candidates for the unattributed review path; do not delete files absent from the pinned baseline.\n")
	}
	return b.String()
}

func writeDiffList(b *strings.Builder, label string, paths []string) {
	if len(paths) == 0 {
		fmt.Fprintf(b, "- %s: none\n", label)
		return
	}
	fmt.Fprintf(b, "- %s:\n", label)
	for _, path := range paths {
		fmt.Fprintf(b, "  - %s\n", path)
	}
}

// DiffFrozenSourcesForRoot loads the current plan snapshot read-only and
// returns the recovery comparison. Pinned SHAs come from the plan itself;
// Main never hand-supplies a hash.
func DiffFrozenSourcesForRoot(root, statePath, journalPath string) (*FrozenSourceDiff, error) {
	store := loopruntime.NewWriter(statePath, journalPath, root, semantic.RuntimeCandidateValidator{})
	current, err := store.Snapshot()
	if err != nil {
		return nil, fmt.Errorf("frozen source diff: snapshot: %w", err)
	}
	plan, _, err := LoadPlan(root, current.State)
	if err != nil {
		return nil, err
	}
	return DiffFrozenSources(root, plan, current.State)
}
