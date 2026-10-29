package cli

import (
	"github.com/entroforge/go-system-builder/internal/policy"
	"github.com/entroforge/go-system-builder/internal/workspace"
	"path/filepath"
	"strings"
)

// savedQuestionnairePath normalizes a successful tool's candidate file path.
// A path is not proof of a write: S2/S3 additionally require native save
// semantics or a matching before/after observation at their original receiver.
// Other stage adapters retain their existing event contracts.
func savedQuestionnairePath(request policy.Input) (string, bool) {
	if request.Event != "PostToolUse" || request.Error != "" {
		return "", false
	}
	// Native read tools expose a path but do not save it. Other tools may
	// expose the path they changed; no tool permission is inferred here.
	switch request.ToolName {
	case "Read", "Glob", "Grep":
		return "", false
	}
	if failed, _ := request.ToolResponse["is_error"].(bool); failed {
		return "", false
	}
	path, _ := request.ToolInput["file_path"].(string)
	if strings.TrimSpace(path) == "" {
		return "", false
	}
	if !filepath.IsAbs(path) {
		if request.CWD == "" {
			return "", false
		}
		path = filepath.Join(request.CWD, path)
	}
	path, err := workspace.CanonicalProspective(path)
	return path, err == nil
}
