package repair

import (
	"fmt"
	"regexp"
	"strings"
)

// NativeS9PrivatePaths identifies the two professional originals of one issued
// Plan responsibility. It never changes the packet's machine input coordinates.
// NativeS9PacketPath preserves historical short IDs and splits a long actual
// responsibility without truncation or content-derived identifiers.
func NativeS9PacketPath(id string) string {
	if len(id) <= 200 {
		return id
	}
	parts := []string{"p"}
	for len(id) > 120 {
		parts = append(parts, id[:120])
		id = id[120:]
	}
	return strings.Join(append(parts, id), "/")
}

func NativeS9PrivatePaths(planPacketID string) (plan, result string, err error) {
	if !regexp.MustCompile(`^packet-[A-Za-z0-9._-]+$`).MatchString(planPacketID) {
		return "", "", fmt.Errorf("invalid original native Plan packet identity")
	}
	dir := ".loop-submissions/native-s9/" + NativeS9PacketPath(planPacketID) + "/"
	return dir + "plan.json", dir + "result.json", nil
}
