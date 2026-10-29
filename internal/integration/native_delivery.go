package integration

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/entroforge/go-system-builder/internal/assignmentrecord"
	"github.com/entroforge/go-system-builder/internal/assignmentresult"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

// Only the two professional files matching the original accepted delivery may
// accompany a clean native worktree. Extra files or edited answers still block.
// No execution registry or second acceptance receipt is created.
func nativeDeliveryProjection(mainRoot, workerRoot string, restoring bool) (map[string]string, error) {
	// Read-only contract detection keeps this adapter inert for frozen legacy
	// work, including checkouts whose untracked Runtime lock would be dirt.
	raw, err := workpacket.ReadFile(mainRoot, ".claude/loop-state.json", 20<<20)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var probe map[string]any
	if err := json.Unmarshal(raw, &probe); err != nil {
		return nil, err
	}
	candidates, err := assignmentrecord.RecordsFromState(probe)
	if err != nil {
		return nil, err
	}
	native := false
	for _, candidate := range candidates {
		native = native || candidate.Contract == assignmentrecord.NativeDeliveryContract
	}
	if !native {
		return nil, nil
	}
	state, err := readCanonicalRuntimeState(mainRoot)
	if err != nil {
		return nil, err
	}
	records, err := assignmentrecord.RecordsFromState(state)
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, r := range records {
		if r.Contract != assignmentrecord.NativeDeliveryContract {
			continue
		}
		current, found, err := assignmentrecord.ForOwner(state, r.OwnerAgentID)
		if err != nil || !found || current.Reference() != r.Reference() {
			continue
		}
		data, err := workpacket.ReadFile(mainRoot, ".claude/assignments/"+r.AssignmentID+".json", 1<<20)
		if err != nil {
			continue
		}
		var coordinate struct {
			WorktreePath string `json:"worktree_path"`
		}
		if json.Unmarshal(data, &coordinate) != nil || filepath.Clean(coordinate.WorktreePath) != filepath.Clean(workerRoot) {
			continue
		}
		ref, err := currentCanonicalResultRef(state, r.OwnerAgentID, r.Task.ID)
		if err != nil {
			return nil, err
		}
		read := func(ref string) ([]byte, error) {
			return workpacket.ReadFile(mainRoot, ref, assignmentresult.EvidenceReadLimit)
		}
		accepted, err := assignmentresult.LoadAccepted(state, r, ref, read)
		if err != nil {
			return nil, err
		}
		plan, err := read(accepted.PlanRef)
		if err != nil {
			return nil, err
		}
		answer, err := json.Marshal(accepted.Answer)
		if err != nil {
			return nil, err
		}
		_, planPath, resultPath := assignmentrecord.NativePaths(r)
		var planMap map[string]any
		if err := json.Unmarshal(plan, &planMap); err != nil {
			return nil, fmt.Errorf("accepted Plan %s: %w", accepted.PlanRef, err)
		}
		for _, item := range []struct {
			path     string
			accepted []byte
			keys     []string
			project  func(map[string]any) error
		}{
			{planPath, plan, []string{"objective", "planned_paths", "assertion_checks", "dependencies", "risks_blockers"}, nil},
			// The receiver stores the expanded Answer while the Worker keeps the
			// compact professional file. Compare the canonical form on both sides.
			{resultPath, answer, []string{"summary", "assertion_evidence", "risks_blockers", "scope_deviations", "closing_assessment"}, func(actual map[string]any) error {
				return assignmentresult.ProjectCompactResult(planMap, actual, len(r.DoneWhen))
			}},
		} {
			raw, err := workpacket.ReadFile(workerRoot, item.path, 1<<20)
			// Stop has already persisted and validated these professional
			// artifacts in Main. Claude Code can remove a no-change worktree
			// before Main integrates; a recovered Git source need not recreate
			// private authoring caches. Present bytes must still match, while
			// pinned Git source, scope and Main checks remain mandatory.
			if os.IsNotExist(err) {
				continue
			}
			if err != nil {
				return nil, err
			}
			var actual, original map[string]json.RawMessage
			if json.Unmarshal(raw, &actual) != nil || actual == nil || json.Unmarshal(item.accepted, &original) != nil {
				return nil, fmt.Errorf("invalid native professional file %s", item.path)
			}
			if item.project != nil {
				projected := map[string]any{}
				for key, value := range actual {
					var decoded any
					if json.Unmarshal(value, &decoded) != nil {
						return nil, fmt.Errorf("invalid native professional file %s", item.path)
					}
					projected[key] = decoded
				}
				if err := item.project(projected); err != nil {
					return nil, fmt.Errorf("native professional file %s differs from accepted delivery: %w", item.path, err)
				}
				encoded, err := json.Marshal(projected)
				if err != nil {
					return nil, err
				}
				if json.Unmarshal(encoded, &actual) != nil {
					return nil, fmt.Errorf("invalid native professional file %s", item.path)
				}
			}
			for key, value := range actual {
				allowed := false
				for _, want := range item.keys {
					if key == want {
						allowed = true
					}
				}
				var a, b bytes.Buffer
				_ = json.Compact(&a, value)
				_ = json.Compact(&b, original[key])
				// JSON object ordering is immaterial; decode and marshal canonically.
				var av, bv any
				_ = json.Unmarshal(a.Bytes(), &av)
				_ = json.Unmarshal(b.Bytes(), &bv)
				aa, _ := json.Marshal(av)
				bb, _ := json.Marshal(bv)
				if !allowed || !bytes.Equal(aa, bb) {
					return nil, fmt.Errorf("native professional file differs from accepted delivery: %s/%s", item.path, key)
				}
			}
			// Missing keys have template defaults; required professional fields
			// must still agree with the accepted original answer.
			for _, key := range item.keys {
				if _, exists := actual[key]; !exists {
					value := bytes.TrimSpace(original[key])
					if len(value) > 0 && !bytes.Equal(value, []byte("null")) && !bytes.Equal(value, []byte("[]")) {
						return nil, fmt.Errorf("native professional file lost field %s", key)
					}
				}
			}
			out[item.path] = workpacket.Digest(raw)
		}
	}
	return out, nil
}
