package workpacket

import (
	"errors"
	"path"
	"strings"
)

// RepairResultResolutionAuthoringPath derives Main's exact authoring filename.
// It is a path projection and grants no identity, receipt or lifecycle authority.
func RepairResultResolutionAuthoringPath(packet RepairResultPacket) (string, error) {
	answer := packet.AnswerPath
	if packet.Binding.Sources.OwnerAgentID == "" || answer == "" || path.IsAbs(answer) || path.Clean(answer) != answer || strings.Contains(answer, "\\") || answer == ".." || strings.HasPrefix(answer, "../") {
		return "", errors.New("S9 resolution requires the registered relative answer path")
	}
	return path.Join(path.Dir(answer), "main-resolution.json"), nil
}
