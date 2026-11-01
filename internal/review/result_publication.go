package review

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/entroforge/go-system-builder/internal/workpacket"
)

// publishResultArtifact runs inside the original Runtime mutation lock. Only
// this call's exclusive creation supplies a candidate; an occupied path is
// never read, adopted or replaced. The original pending/receipt registers the
// actual returned path, and an interruption before that leaves unknown data.
func publishResultArtifact(ctx context.Context, root string, state map[string]any, preferred string, data []byte) (string, error) {
	if filepath.ToSlash(filepath.Clean(preferred)) != preferred || !strings.HasPrefix(preferred, ".claude/evidence/") {
		return "", errors.New("review artifact path is outside canonical evidence scope")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	// Check only the essential storage parent, never a candidate's contents.
	parent, _, err := workpacket.OpenParent(root, ".claude/evidence/review-publication-probe", true)
	if err != nil {
		return "", err
	}
	parent.Close()
	path := preferred
	for {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		// An existing registered source remains reserved even when its file
		// is temporarily missing. Its absence grants no replacement authority.
		if !registeredReviewArtifactPath(state, path) {
			created, err := workpacket.PublishNewBounded(root, path, data, int64(len(data)))
			if err != nil {
				return "", err
			}
			if created {
				return path, nil
			}
		}
		var token [16]byte
		if _, err := rand.Read(token[:]); err != nil {
			return "", err
		}
		path = fmt.Sprintf(".claude/evidence/review-candidate-%x/artifact.json", token)
	}
}

// Consult only original Runtime reference fields, never user prose or files.
func registeredReviewArtifactPath(state map[string]any, path string) bool {
	rows, _ := state["evidence"].([]any)
	for _, raw := range rows {
		row, _ := raw.(map[string]any)
		if row["path"] == path {
			return true
		}
	}
	entities, _ := state["entities"].(map[string]any)
	rows, _ = entities["findings"].([]any)
	for _, raw := range rows {
		row, _ := raw.(map[string]any)
		if row["path"] == path {
			return true
		}
	}
	review, _ := state["review"].(map[string]any)
	assignments, _ := review["assignments"].(map[string]any)
	for _, raw := range assignments {
		row, _ := raw.(map[string]any)
		receipt, _ := row["result_receipt"].(map[string]any)
		if row["result_ref"] == path || receipt["result_ref"] == path {
			return true
		}
	}
	rows, _ = entities["work_packets"].([]any)
	for _, raw := range rows {
		row, _ := raw.(map[string]any)
		reception, _ := row["review_result_reception"].(map[string]any)
		receipt, _ := reception["receipt"].(map[string]any)
		if receipt["result_ref"] == path {
			return true
		}
	}
	return false
}
