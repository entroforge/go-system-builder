package cli

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/entroforge/go-system-builder/internal/checkexec"
	"github.com/entroforge/go-system-builder/internal/classifier"
	"github.com/entroforge/go-system-builder/internal/filelock"
	"github.com/entroforge/go-system-builder/internal/hook"
	"github.com/entroforge/go-system-builder/internal/policy"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

func progressStatusCommand(root string, request policy.Input, execution *workspace.Execution) bool {
	command := progressText(request.ToolInput["command"])
	if execution != nil {
		if command == workspace.ReceiptInvocation(*execution) || command == workspace.WorkerInvocation(*execution, "receipt") {
			return true
		}
		for _, value := range workspace.ObservationCommands(*execution) {
			if command == value {
				return true
			}
		}
	}
	tokens, err := classifier.Tokenize(command)
	if err != nil || len(tokens) == 0 {
		return false
	}
	args := []string{}
	for _, token := range tokens {
		switch token.Kind {
		case classifier.TkWord, classifier.TkShortFlag, classifier.TkLongFlag, classifier.TkValue, classifier.TkQuotedString:
		default:
			return false
		}
		if strings.ContainsAny(token.Value, "$\n\r") || strings.ContainsRune(token.Value, rune(96)) {
			return false
		}
		args = append(args, token.Value)
	}
	if execution == nil && request.CWD == root && len(args) >= 2 && args[0] == "git" && args[1] == "status" && (len(args) == 2 || (len(args) == 3 && args[2] == "--short")) {
		return true
	}
	if args[0] != filepath.Join(root, ".claude/bin/loop-harness") && args[0] != ".claude/bin/loop-harness" {
		return false
	}
	if len(args) < 2 || (args[1] != "status" && args[1] != "next" && !(len(args) >= 3 && args[1] == "s6" && args[2] == "status")) {
		return false
	}
	n := 2
	if args[1] == "s6" {
		n = 3
	}
	for i := n; i < len(args); i++ {
		switch args[i] {
		case "--root":
			if i+1 >= len(args) || filepath.Clean(args[i+1]) != filepath.Clean(root) {
				return false
			}
			i++
		case "--json", "--advice":
		default:
			return false
		}
	}
	return execution == nil
}

// A supported response must prove actual returned bytes, not requested extent.
// Missing/truncated response data is incomplete observation, not stagnation.
func actualProgressRead(root string, request policy.Input, sources map[string]string) *hook.ProgressRead {
	path := progressText(request.ToolInput["file_path"])
	if !filepath.IsAbs(path) {
		path = filepath.Join(request.CWD, path)
	}
	path = filepath.Clean(path)
	if sources[path] == "" {
		return nil
	}
	file, ok := request.ToolResponse["file"].(map[string]any)
	if !ok || filepath.Clean(progressText(file["filePath"])) != path {
		return nil
	}
	integer := func(v any) int {
		n, ok := v.(float64)
		if !ok || n < 1 || n > 1000000 || n != float64(int(n)) {
			return 0
		}
		return int(n)
	}
	start, count := integer(file["startLine"]), integer(file["numLines"])
	if start == 0 || count == 0 {
		return nil
	}
	readRoot := root
	rel, err := filepath.Rel(readRoot, path)
	if err != nil || !filepath.IsLocal(rel) {
		readRoot = request.CWD
		rel, err = filepath.Rel(readRoot, path)
	}
	if err != nil || !filepath.IsLocal(rel) {
		return nil
	}
	data, err := workpacket.ReadFile(readRoot, filepath.ToSlash(rel), 4<<20)
	if err != nil || workpacket.Digest(data) != sources[path] {
		return nil
	}
	lines := strings.SplitAfter(string(data), "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	if start-1+count > len(lines) || strings.Join(lines[start-1:start-1+count], "") != progressText(file["content"]) {
		return nil
	}
	return &hook.ProgressRead{Path: path, Digest: sources[path], Range: fmt.Sprintf("%d:%d", start, count)}
}

// Only original professional artifact envelopes lose their known top-level
// observer metadata. Product JSON fields retain their full meaning.
func progressProfessionalArtifact(root, ref string) (string, error) {
	data, err := workpacket.ReadFile(root, filepath.ToSlash(ref), 256<<10)
	if os.IsNotExist(err) {
		return "missing", nil
	}
	if err != nil {
		return "", err
	}
	var value map[string]any
	if json.Unmarshal(data, &value) != nil {
		return workpacket.Digest(data), nil
	}
	for _, key := range []string{"updated_at", "occurred_at", "timestamp", "message_id", "session_id"} {
		delete(value, key)
	}
	return workpacket.Digest([]byte(progressJSON(value))), nil
}

func progressOwnedCode(root string, patterns []string, budget *int64) (map[string]string, error) {
	hashes := map[string]string{}
	visited := 0
	for _, pattern := range patterns {
		prefix, _, wild := strings.Cut(pattern, "*")
		start := prefix
		if wild {
			start = strings.TrimSuffix(prefix, "/")
			if !strings.HasSuffix(prefix, "/") {
				start = filepath.Dir(prefix)
			}
		}
		if !filepath.IsLocal(start) {
			return nil, fmt.Errorf("owned source scope cannot be observed literally")
		}
		if strings.HasPrefix(filepath.ToSlash(start), ".claude/") || strings.HasPrefix(filepath.ToSlash(start), ".git") {
			continue
		}
		err := filepath.WalkDir(filepath.Join(root, filepath.FromSlash(start)), func(path string, entry fs.DirEntry, walkErr error) error {
			if entry != nil && entry.IsDir() {
				switch entry.Name() {
				case ".git", ".claude", ".local", ".worktrees", ".codex":
					return filepath.SkipDir
				}
			}
			visited++
			if visited > 256 {
				return fmt.Errorf("owned source observation exceeds 256 entries")
			}
			if os.IsNotExist(walkErr) {
				return nil
			}
			if walkErr != nil {
				return walkErr
			}
			if entry.Type()&os.ModeSymlink != 0 {
				return fmt.Errorf("owned source observation refuses links")
			}
			if entry.IsDir() {
				return nil
			}
			ref, err := filepath.Rel(root, path)
			if err != nil || !workspace.PathMatchesScope(filepath.ToSlash(ref), pattern) {
				return err
			}
			if _, seen := hashes[ref]; seen {
				return nil
			}
			data, err := workpacket.ReadFile(root, filepath.ToSlash(ref), *budget)
			if err != nil {
				return err
			}
			*budget -= int64(len(data))
			hashes[ref] = workpacket.Digest(data)
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return hashes, nil
}

func progressCheckRunning(root string, e workspace.Execution) (bool, error) {
	base := filepath.Join(root, ".claude/evidence/check-runs")
	entries, err := os.ReadDir(base)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if len(entries) > 128 {
		return false, fmt.Errorf("running check coverage exceeds 128 entries")
	}
	for _, entry := range entries {
		if !entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		r, found, err := checkexec.ReadRunRecord(filepath.Join(base, entry.Name(), "run.json"))
		if err != nil {
			return false, err
		}
		if !found || r.Binding != workspace.CheckBinding(e) {
			continue
		}
		_, busy, err := filelock.Probe(filepath.Join(base, entry.Name(), "run.lock"))
		if err != nil {
			return false, err
		}
		if busy {
			return true, nil
		}
	}
	return false, nil
}
