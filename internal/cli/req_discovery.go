package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// reqSummary is one scanned REQ file under docs/requirements/.
type reqSummary struct {
	ID      string `json:"id"`
	Path    string `json:"path"`
	Status  string `json:"status"`
	Version string `json:"version"`
	// Bindable reports whether this REQ may be bound right now: locked, not
	// bound by the current runtime, and not closed by an archived runtime.
	Bindable bool   `json:"bindable"`
	Note     string `json:"note,omitempty"`
}

// scanRequirements lists REQ-*.md files (templates excluded) with their
// top-of-file 状态/Version fields. An empty result is a valid answer: S0 has
// not produced a REQ yet.
func scanRequirements(root string) []reqSummary {
	matches, _ := filepath.Glob(filepath.Join(root, "docs", "requirements", "REQ-*.md"))
	summaries := make([]reqSummary, 0, len(matches))
	for _, abs := range matches {
		base := filepath.Base(abs)
		if strings.Contains(strings.ToLower(base), "template") {
			continue
		}
		data, err := os.ReadFile(abs)
		if err != nil {
			continue
		}
		summaries = append(summaries, reqSummary{
			ID:      strings.TrimSuffix(base, filepath.Ext(base)),
			Path:    filepath.ToSlash(filepath.Join("docs", "requirements", base)),
			Status:  markdownField(string(data), "状态", "Status"),
			Version: markdownField(string(data), "版本", "Version"),
		})
	}
	sort.Slice(summaries, func(i, j int) bool { return summaries[i].ID < summaries[j].ID })
	return summaries
}

// archivedBoundIDs returns REQ ids referenced by archived runtimes whose
// lifecycle closed terminally (release_authorized / aborted). Those REQs are
// done; auto-discovery must not surface them again. Unbound archives (a later
// disposition) are not collected here — an unbound REQ returns to the pool.
func archivedBoundIDs(root string) map[string]bool {
	ids := map[string]bool{}
	entries, _ := os.ReadDir(filepath.Join(root, ".claude", "runtime-archive"))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(root, ".claude", "runtime-archive", entry.Name(), "loop-state.json"))
		if err != nil {
			continue
		}
		var state map[string]any
		if json.Unmarshal(data, &state) != nil {
			continue
		}
		lifecycle, _ := state["lifecycle"].(map[string]any)
		stateName, _ := lifecycle["state"].(string)
		if stateName != "release_authorized" && stateName != "aborted" {
			continue
		}
		bound, _ := state["bound_req"].(map[string]any)
		if id, _ := bound["id"].(string); id != "" {
			ids[id] = true
		}
	}
	return ids
}

// currentBoundID returns the REQ id bound to the on-disk runtime, or "" when
// no runtime file exists or nothing is bound.
func currentBoundID(root string) string {
	data, err := os.ReadFile(filepath.Join(root, ".claude", "loop-state.json"))
	if err != nil {
		return ""
	}
	var state map[string]any
	if json.Unmarshal(data, &state) != nil {
		return ""
	}
	bound, _ := state["bound_req"].(map[string]any)
	id, _ := bound["id"].(string)
	return id
}

// classifyRequirements annotates scanned REQs with bindability and a
// human-readable reason. The authority is the runtime and runtime-archive —
// REQ file status alone never decides (locked means "frozen baseline", not
// "in progress").
func classifyRequirements(root string) []reqSummary {
	bound := currentBoundID(root)
	archived := archivedBoundIDs(root)
	summaries := scanRequirements(root)
	for i := range summaries {
		s := &summaries[i]
		switch {
		case s.Status == "archived":
			s.Note = "lifecycle closed (archived REQ; see runtime-archive)"
		case s.Status != "locked":
			s.Note = "not bindable: status is " + s.Status + " (lock it in S0 first)"
		case s.ID == bound:
			s.Note = "currently bound to this runtime"
		case archived[s.ID]:
			s.Note = "lifecycle closed (archived runtime)"
		default:
			s.Bindable = true
			s.Note = "bindable"
		}
	}
	return summaries
}

// bindableOnly returns the bindable subset of classifyRequirements.
func bindableOnly(root string) []reqSummary {
	var out []reqSummary
	for _, s := range classifyRequirements(root) {
		if s.Bindable {
			out = append(out, s)
		}
	}
	return out
}

// soleBindableCommand returns the ready-to-run bind command when exactly one
// REQ is bindable, for projection hints. Empty string otherwise; scan issues
// degrade to silence rather than blocking projection.
func soleBindableCommand(root string) string {
	candidates := bindableOnly(root)
	if len(candidates) != 1 {
		return ""
	}
	return "loop-harness req bind --req " + candidates[0].Path + " --approved-by <the human who locked it>"
}

// detectGitIdentity returns the local git user.name for the --approved-by
// hint. Empty when unavailable; the attestation itself stays explicit.
func detectGitIdentity(root string) string {
	out, err := exec.Command("git", "-C", root, "config", "user.name").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// tolerantInt extracts an integer from a JSON-ish map value that may be a
// float64 (decoded JSON) or an int (freshly constructed state).
func tolerantInt(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case int64:
		return int(n)
	default:
		return 0
	}
}

// printBindConfirmation renders the human-facing success output. Every fact
// shown here has a journal counterpart (D1/D6: output and journal are
// mutually verifiable).
func printBindConfirmation(w io.Writer, state map[string]any) {
	bound, _ := state["bound_req"].(map[string]any)
	lifecycle, _ := state["lifecycle"].(map[string]any)
	baseline, _ := state["baseline"].(map[string]any)
	last, _ := state["last_transition"].(map[string]any)
	id, _ := bound["id"].(string)
	version, _ := bound["version"].(string)
	sha, _ := bound["sha256"].(string)
	approvedBy, _ := bound["approved_by"].(string)
	meta, _ := bound["metadata"].(map[string]any)
	ui, _ := meta["ui_impact"].(string)
	if len(sha) > 12 {
		sha = sha[:12]
	}
	event, _ := last["event"].(string)
	if receipt, ok := state["binding_receipt"].(map[string]any); ok {
		event, _ = receipt["event"].(string)
	}
	fmt.Fprintf(w, "bound %s %s (ui_impact=%s)\n", id, version, ui)
	fmt.Fprintf(w, "  sha256 %s…  approved-by %s\n", sha, approvedBy)
	fmt.Fprintf(w, "  cursor %s.%s  revision %d  generation %d  event %s\n",
		lifecycle["state"], lifecycle["phase"], tolerantInt(state["revision"]), tolerantInt(baseline["generation"]), event)
	fmt.Fprintln(w, "next: S2 design — hooks project status automatically; no further CLI needed.")
}
