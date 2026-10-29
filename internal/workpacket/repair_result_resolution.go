package workpacket

import (
	"errors"
	"path"
	"strings"
)

// RepairResultResolutionAuthoringPath derives Main's exact authoring filename.
// It is a path projection and grants no identity, receipt or lifecycle authority.
func RepairResultResolutionAuthoringPath(packet RepairResultPacket) (string, error) {
	owner, answer := packet.Binding.Sources.OwnerAgentID, packet.AnswerPath
	suffix := "/" + owner + "/answer.json"
	if owner == "" || answer == "" || path.IsAbs(answer) || path.Clean(answer) != answer || strings.Contains(answer, "\\") || !strings.HasSuffix(answer, suffix) {
		return "", errors.New("S9 resolution requires the exact registered <owner>/answer.json path")
	}
	return strings.TrimSuffix(answer, "answer.json") + "main-resolution.json", nil
}
