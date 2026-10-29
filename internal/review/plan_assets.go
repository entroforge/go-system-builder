package review

import (
	"context"
	"encoding/json"
	"fmt"
	"path"
	"reflect"
	"sort"
	"strings"

	"github.com/entroforge/go-system-builder/internal/fileview"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

// preparePlanAssets fills only omitted asset machine fields. Unknown fields
// stay in the original JSON for the formal schema to reject. It never writes.
func preparePlanAssets(root string, data []byte, state map[string]any, contexts ...context.Context) ([]byte, error) {
	ctx := context.Background()
	if len(contexts) > 0 && contexts[0] != nil {
		ctx = contexts[0]
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(data) > 20<<20 {
		return nil, &PlanAnswerError{fmt.Errorf("ReviewPlan exceeds 20 MiB")}
	}
	if err := workpacket.ValidateUniqueJSON(data); err != nil {
		return nil, &PlanAnswerError{err}
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, &PlanAnswerError{err}
	}
	var plan Plan
	if err := json.Unmarshal(data, &plan); err != nil {
		return nil, &PlanAnswerError{err}
	}
	if len(plan.E2EAssets) == 0 {
		return data, nil
	}
	read, err := planAssetReader(ctx, root, &plan, state)
	if err != nil {
		return nil, err
	}
	var rows []map[string]json.RawMessage
	if err := json.Unmarshal(raw["e2e_assets"], &rows); err != nil {
		return nil, err
	}
	for i, asset := range plan.E2EAssets {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		ref, err := resolveAssetCase(asset.CaseRef, &plan, read)
		if err != nil {
			return nil, fmt.Errorf("E2E asset %s: %w", asset.Path, err)
		}
		body, err := read(asset.Path)
		if err != nil {
			return nil, fmt.Errorf("read selected E2E asset %s: %w", asset.Path, err)
		}
		sha := sha256Of(body)
		if asset.SHA256 != "" && asset.SHA256 != sha {
			return nil, fmt.Errorf("S7_E2E_ASSET_FINGERPRINT: asset %s is stale; preserve the pinned source instead of refreshing its declared digest", asset.Path)
		}
		if asset.AssetID == "" {
			asset.AssetID = "e2e-asset:" + sha256Of([]byte(ref + "\n" + asset.Path))[:20]
		}
		for key, value := range map[string]string{"case_ref": ref, "asset_id": asset.AssetID, "sha256": sha} {
			rows[i][key], _ = json.Marshal(value)
		}
	}
	raw["e2e_assets"], err = json.Marshal(rows)
	if err != nil {
		return nil, err
	}
	return json.Marshal(raw)
}

// Canonical plans use the original source declaration at the pinned Main
// commit. Historical disk-mode plans keep their existing contained disk view.
func planAssetReader(ctx context.Context, root string, plan *Plan, state map[string]any) (func(string) ([]byte, error), error) {
	disk := func(p string) ([]byte, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return workpacket.ReadFile(root, p, 2<<20)
	}
	if plan.ProductBaseline == nil {
		return disk, nil
	}
	definition, _ := state["definition"].(map[string]any)
	data, err := workpacket.ReadFile(root, stringField(definition["path"]), 16<<20)
	if err != nil || sha256Of(data) != stringField(definition["sha256"]) {
		return nil, fmt.Errorf("original source declaration is missing or changed; Main must restore preparation")
	}
	var declaration struct {
		FileSources []fileview.Rule `json:"file_sources"`
	}
	if err := json.Unmarshal(data, &declaration); err != nil {
		return nil, err
	}
	view, err := fileview.NewContext(ctx, root, plan.ProductBaseline.FinalCommit, declaration.FileSources)
	if err != nil {
		return nil, err
	}
	return func(p string) ([]byte, error) {
		if err := workpacket.CheckPath(root, p); err != nil {
			return nil, err
		}
		data, err := view.ReadFile(p)
		if len(data) > 2<<20 {
			return nil, fmt.Errorf("selected source %s exceeds 2 MiB", p)
		}
		return data, err
	}, nil
}

func caseReferenceParts(ref string) (string, string, error) {
	p, id, ok := strings.Cut(ref, "#")
	if !ok || id == "" || strings.ContainsAny(id, "#/\\") || p != path.Clean(p) || !strings.HasPrefix(p, "docs/design/prototypes/") || path.Base(p) != "cases.json" {
		return "", "", &PlanAnswerError{fmt.Errorf("use an exact cases.json#CASE reference, got %q", ref)}
	}
	return p, id, nil
}

func declaredCasePaths(plan *Plan) []string {
	paths := map[string]bool{}
	for _, c := range plan.Claims {
		if c.Lens == "e2e" {
			for _, ref := range c.SourceRefs {
				p, _, _ := strings.Cut(ref, "#")
				if strings.HasPrefix(p, "docs/design/prototypes/") && path.Base(p) == "cases.json" {
					paths[p] = true
				}
			}
		}
	}
	for _, s := range plan.FrozenSubjects {
		if strings.HasPrefix(s.Path, "docs/design/prototypes/") && path.Base(s.Path) == "cases.json" {
			paths[s.Path] = true
		}
	}
	out := make([]string, 0, len(paths))
	for p := range paths {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

func resolveAssetCase(ref string, plan *Plan, read func(string) ([]byte, error)) (string, error) {
	var paths []string
	id := ref
	if strings.Contains(ref, "#") {
		p, c, err := caseReferenceParts(ref)
		if err != nil {
			return "", err
		}
		declared := false
		for _, source := range declaredCasePaths(plan) {
			declared = declared || source == p
		}
		if !declared {
			return "", &PlanAnswerError{fmt.Errorf("scenario %s is outside the Plan's declared CASE sources", p)}
		}
		paths = []string{p}
		id = c
	} else {
		paths = declaredCasePaths(plan)
	}
	var matches []string
	for _, p := range paths {
		data, err := read(p)
		if err != nil {
			return "", fmt.Errorf("scenario source %s: %w", p, err)
		}
		var cases e2eCasesDocument
		if err := json.Unmarshal(data, &cases); err != nil {
			return "", fmt.Errorf("scenario source %s: %w", p, err)
		}
		found := 0
		for _, c := range cases.Cases {
			if c.ID == id {
				found++
			}
		}
		if found > 1 {
			return "", fmt.Errorf("duplicate CASE %s in %s; correct the original source", id, p)
		}
		if found == 1 {
			matches = append(matches, p+"#"+id)
		}
	}
	if len(matches) != 1 {
		return "", &PlanAnswerError{fmt.Errorf("CASE %q has %d matches in the declared scenario sources (%s); Main must select the exact cases.json#CASE reference", ref, len(matches), strings.Join(matches, ", "))}
	}
	return matches[0], nil
}

// SamePlanAnswer recognizes system-filled fields using only the original
// accepted plan. It never refreshes a source or accepts changed professional
// content, so an already accepted answer survives later filesystem drift.
func SamePlanAnswer(authored, accepted []byte) bool {
	if workpacket.ValidateUniqueJSON(authored) != nil {
		return false
	}
	var a, b map[string]any
	if json.Unmarshal(authored, &a) != nil || json.Unmarshal(accepted, &b) != nil {
		return false
	}
	ar, _ := a["e2e_assets"].([]any)
	br, _ := b["e2e_assets"].([]any)
	if len(ar) != len(br) {
		return false
	}
	var plan Plan
	if json.Unmarshal(accepted, &plan) != nil {
		return false
	}
	for i := range ar {
		x, ok := ar[i].(map[string]any)
		if !ok {
			return false
		}
		y, ok := br[i].(map[string]any)
		if !ok {
			return false
		}
		for _, key := range []string{"asset_id", "sha256"} {
			if x[key] == nil || x[key] == "" {
				x[key] = y[key]
			}
		}
		if ref, ok := x["case_ref"].(string); ok && !strings.Contains(ref, "#") {
			matches := map[string]bool{}
			var exact string
			for _, asset := range plan.E2EAssets {
				_, id, err := caseReferenceParts(asset.CaseRef)
				if err == nil && id == ref {
					matches[asset.CaseRef] = true
					exact = asset.CaseRef
				}
			}
			if len(matches) == 1 {
				x["case_ref"] = exact
			}
		}
	}
	return reflect.DeepEqual(a, b)
}
