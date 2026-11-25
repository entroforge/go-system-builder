package repair

import (
	"fmt"
	"regexp"

	"github.com/entroforge/go-system-builder/internal/workpacket"
)

// NativeS9PrivatePaths identifies the two professional originals of one issued
// Plan responsibility. It never changes the packet's machine input coordinates.
//
// NativeS9PacketPath preserves historical short IDs. A long actual
// responsibility maps to a bounded content-hash directory key: a repair
// session id embeds its whole encoded parent chain, and verbatim chopping —
// or the verbatim single component this regex admits — exceeds the kernel
// path limits once repair chains inherit their parents. The full id remains
// in the Runtime registration and the packet JSON; only the directory key is
// bounded.
func NativeS9PacketPath(id string) string {
	if len(id) <= 200 {
		return id
	}
	return "p/" + workpacket.BoundedPacketPathKey(id)
}

func NativeS9PrivatePaths(planPacketID string) (plan, result string, err error) {
	if !regexp.MustCompile(`^packet-[A-Za-z0-9._-]+$`).MatchString(planPacketID) {
		return "", "", fmt.Errorf("invalid original native Plan packet identity")
	}
	dir := ".loop-submissions/native-s9/" + NativeS9PacketPath(planPacketID) + "/"
	return dir + "plan.json", dir + "result.json", nil
}
