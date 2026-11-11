package submission

import (
	"encoding/json"
	"fmt"
	"path/filepath"

	"github.com/entroforge/go-system-builder/internal/workpacket"
)

// A candidate path is not an issuance or a claim on a previous file. The
// Runtime writer records whichever exclusively created path wins issuance.
func (s *Service) reserveS5Answer(packet workpacket.Packet) (workpacket.Packet, error) {
	data, err := json.MarshalIndent(packet.Draft, "", "  ")
	if err != nil {
		return packet, err
	}
	data = append(data, '\n')
	for n := 0; ; n++ {
		path := packet.AnswerPath
		if n > 0 {
			path = filepath.ToSlash(filepath.Join(".claude/submissions", fmt.Sprintf("s5-candidate-%d", n), packet.Binding.Owner, "answer.json"))
		}
		created, err := workpacket.PublishNewBounded(s.Root, path, data, 1<<20)
		if err != nil {
			return packet, err
		}
		if created {
			packet.AnswerPath = path
			return packet, nil
		}
		if err := workpacket.CheckPath(s.Root, ".claude/submissions/s5-publication-probe/answer.json"); err != nil {
			return packet, fmt.Errorf("S5 answer storage unavailable: %w", err)
		}
	}
}

func (s *Service) reserveS5View(packet workpacket.Packet) (workpacket.Packet, error) {
	for n := 0; ; n++ {
		path := packet.ViewPath
		if n > 0 {
			path = filepath.ToSlash(filepath.Join(".claude/work-packets", fmt.Sprintf("s5-candidate-%d", n), "view.json"))
		}
		packet.ViewPath = path
		data, err := json.MarshalIndent(packet, "", "  ")
		if err != nil {
			return packet, err
		}
		created, err := workpacket.PublishNewBounded(s.Root, path, append(data, '\n'), 1<<20)
		if err != nil {
			return packet, err
		}
		if created {
			return packet, nil
		}
		if err := workpacket.CheckPath(s.Root, ".claude/work-packets/s5-publication-probe/view.json"); err != nil {
			return packet, fmt.Errorf("S5 view storage unavailable: %w", err)
		}
	}
}

func sameS5Responsibility(a, b workpacket.Packet) bool {
	x, y := a.Binding, b.Binding
	return a.ID == b.ID && a.TemplateID == b.TemplateID &&
		x.RuntimeID == y.RuntimeID && x.Generation == y.Generation &&
		x.StageEntry == y.StageEntry && x.Owner == y.Owner &&
		x.AssignmentID == y.AssignmentID && x.Responsibility == y.Responsibility &&
		x.Attempt == y.Attempt && x.PreviousSubmissionKey == y.PreviousSubmissionKey
}
