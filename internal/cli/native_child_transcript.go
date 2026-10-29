package cli

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/entroforge/go-system-builder/internal/policy"
)

// Resolve the real platform child file from the observed parent transcript
// coordinate and actor. Parent text, a guessed owner and other child files are
// never searched for a matching responsibility.
func nativeChildTranscript(in policy.Input) (string, bool) {
	if in.AgentID == "" || in.AgentID == "Main" || strings.ContainsAny(in.AgentID, "/\\") || in.AgentID == "." || in.AgentID == ".." {
		return "", false
	}
	path := in.AgentTranscriptPath
	if path == "" {
		if !filepath.IsAbs(in.TranscriptPath) || filepath.Ext(in.TranscriptPath) != ".jsonl" {
			return "", false
		}
		session := strings.TrimSuffix(in.TranscriptPath, ".jsonl")
		path = filepath.Join(session, "subagents", "agent-"+in.AgentID+".jsonl")
	}
	file, err := os.Open(path)
	if err != nil {
		return "", false
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return "", false
	}
	scanner := bufio.NewScanner(io.LimitReader(file, 128<<10))
	scanner.Buffer(make([]byte, 4096), 128<<10)
	for scanner.Scan() {
		if contextForHook(in).Err() != nil {
			return "", false
		}
		var row struct {
			Sidechain bool   `json:"isSidechain"`
			AgentID   string `json:"agentId"`
		}
		if json.Unmarshal(scanner.Bytes(), &row) == nil && row.Sidechain && row.AgentID == in.AgentID {
			return path, true
		}
	}
	return "", false
}
