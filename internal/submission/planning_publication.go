package submission

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/entroforge/go-system-builder/internal/workpacket"
)

// publishPlanningFiles creates the view and answer exclusively before a new
// packet is registered. Occupied paths remain untouched and never establish
// ownership. Registered packets reuse their original paths on replay.
func publishPlanningFiles(root string, registered bool, originalView, originalAnswer string, limit int64, build func(string, string) (any, any)) (string, string, error) {
	for attempt := 0; ; attempt++ {
		view, answer := originalView, originalAnswer
		if attempt > 0 {
			name := fmt.Sprintf("planning-candidate-%d", attempt)
			view = filepath.ToSlash(filepath.Join(".claude/work-packets", name, "view.json"))
			stage := ""
			for _, candidate := range []string{"S3", "S4"} {
				if strings.Contains(originalAnswer, "/"+candidate+"/") {
					stage = candidate
					break
				}
			}
			answer = filepath.ToSlash(filepath.Join(".claude/submissions", name, stage, "Main", "answer.json"))
		}
		packet, draft := build(view, answer)
		viewBytes, err := json.MarshalIndent(packet, "", "  ")
		if err != nil {
			return "", "", err
		}
		draftBytes, err := json.MarshalIndent(draft, "", "  ")
		if err != nil {
			return "", "", err
		}
		viewBytes = append(viewBytes, '\n')
		draftBytes = append(draftBytes, '\n')
		created, err := workpacket.PublishNewBounded(root, view, viewBytes, int64(len(viewBytes)))
		if err != nil {
			return "", "", err
		}
		if !created && !registered {
			if err := workpacket.CheckPath(root, ".claude/work-packets/planning-publication-probe/view.json"); err != nil {
				return "", "", fmt.Errorf("planning view storage unavailable: %w", err)
			}
			continue
		}
		created, err = workpacket.PublishNewBounded(root, answer, draftBytes, limit)
		if err != nil {
			return "", "", err
		}
		if !created && !registered {
			if err := workpacket.CheckPath(root, ".claude/submissions/planning-publication-probe/Main/answer.json"); err != nil {
				return "", "", fmt.Errorf("planning answer storage unavailable: %w", err)
			}
			continue
		}
		return view, answer, nil
	}
}
