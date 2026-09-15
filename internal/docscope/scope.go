// Package docscope separates explicit document ownership from incidental
// references to older specifications in the body of a document.
package docscope

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var reqID = regexp.MustCompile(`REQ-[A-Za-z0-9]+(?:-[A-Za-z0-9]+)*`)

// Belongs retains unlabelled legacy documents for compatibility. Explicit
// ownership, when declared, is authoritative; body references are not owners.
func Belongs(data []byte, bound string) bool {
	if bound == "" {
		return true
	}
	return len(Owners(data)) == 0 || Owners(data)[bound]
}

func Owners(data []byte) map[string]bool {
	owners := map[string]bool{}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "## ") {
			break
		}
		if !strings.HasPrefix(line, ">") {
			continue
		}
		line = strings.TrimSpace(strings.TrimPrefix(line, ">"))
		line = strings.ReplaceAll(line, "：", ":")
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(parts[0])) {
		case "source req refs", "需求", "关联需求", "source req", "req":
			for _, id := range reqID.FindAllString(parts[1], -1) {
				owners[id] = true
			}
		}
	}
	return owners
}
func Bound(root string) string {
	b, err := os.ReadFile(filepath.Join(root, ".claude/loop-state.json"))
	if err != nil {
		return ""
	}
	var s struct {
		Bound struct {
			ID string `json:"id"`
		} `json:"bound_req"`
	}
	if json.Unmarshal(b, &s) != nil {
		return ""
	}
	return s.Bound.ID
}
