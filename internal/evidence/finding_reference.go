package evidence

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// ParsePathReference is shared with S7. A missing digest is returned to the
// caller so the original S7 receiver can supply its specific repair guidance.
func ParsePathReference(ref string) (string, string, error) {
	rel := strings.TrimPrefix(ref, "path:")
	index := strings.Index(rel, "#sha256=")
	if index < 0 {
		return rel, "", nil
	}
	want := rel[index+len("#sha256="):]
	if len(want) != 64 {
		return "", "", fmt.Errorf("#sha256 must contain exactly 64 hexadecimal characters")
	}
	if _, err := hex.DecodeString(want); err != nil {
		return "", "", fmt.Errorf("#sha256 is not hexadecimal: %w", err)
	}
	return rel[:index], want, nil
}

var commandOutputReference = regexp.MustCompile(`^command_output:(.+)#sha256=([0-9a-f]{64}) \(bytes=([0-9]+), truncated=(true|false)\)$`)

type CommandOutputReference struct {
	RelativePath string
	SHA256       string
	Bytes        int64
	Truncated    bool
	Withheld     bool
}

func ParseCommandOutputReference(ref string) (CommandOutputReference, error) {
	ref = strings.TrimSpace(ref)
	if !strings.HasPrefix(ref, "command_output:") {
		return CommandOutputReference{}, fmt.Errorf("not a command_output reference")
	}
	body := strings.TrimPrefix(ref, "command_output:")
	if strings.Contains(body, " (withheld:") {
		return CommandOutputReference{RelativePath: strings.TrimSpace(strings.SplitN(body, " (withheld:", 2)[0]), Withheld: true}, nil
	}
	parts := commandOutputReference.FindStringSubmatch(ref)
	if len(parts) != 5 {
		return CommandOutputReference{}, fmt.Errorf("command_output reference must include #sha256, bytes and truncated metadata")
	}
	count, err := strconv.ParseInt(parts[3], 10, 64)
	if err != nil || count < 0 {
		return CommandOutputReference{}, fmt.Errorf("command_output bytes metadata is invalid")
	}
	return CommandOutputReference{RelativePath: parts[1], SHA256: parts[2], Bytes: count, Truncated: parts[4] == "true"}, nil
}

// FindingReference describes only the content declared by an original S7
// reference. Redacted/deleted/environment observations retain their meaning
// without inventing bytes, an index entry, or a new professional conclusion.
type FindingReference struct {
	Path   string
	SHA256 string
}

func ParseFindingReference(ref string) (FindingReference, bool, error) {
	var path, digest string
	var err error
	switch {
	case strings.HasPrefix(ref, "path:"):
		path, digest, err = ParsePathReference(ref)
		if err == nil && (path == "" || digest == "") {
			err = fmt.Errorf("path reference requires a relative file and #sha256 digest")
		}
	case strings.HasPrefix(ref, "command_output:"):
		var output CommandOutputReference
		output, err = ParseCommandOutputReference(ref)
		if output.Withheld {
			return FindingReference{}, true, err
		}
		path, digest = output.RelativePath, output.SHA256
	case strings.HasPrefix(ref, "artifact:"):
		body := strings.TrimPrefix(ref, "artifact:")
		if strings.HasSuffix(body, " (deleted)") {
			if strings.TrimSpace(strings.TrimSuffix(body, " (deleted)")) == "" {
				return FindingReference{}, true, fmt.Errorf("empty deleted artifact reference")
			}
			return FindingReference{}, true, nil
		}
		// Artifact captures describe an observation, not verified live bytes.
		path, digest, err = ParsePathReference("path:" + strings.SplitN(body, " ", 2)[0])
		if err == nil && (path == "" || digest == "" || filepath.IsAbs(path) || strings.HasPrefix(filepath.ToSlash(filepath.Clean(path)), "../")) {
			err = fmt.Errorf("artifact reference requires a repository-relative path and sha256 digest")
		}
		return FindingReference{}, true, err
	case strings.HasPrefix(ref, "env:"):
		body := strings.TrimSpace(strings.TrimPrefix(ref, "env:"))
		if body == "" || !strings.Contains(body, " (present; value never captured)") || strings.TrimSpace(strings.SplitN(body, " (present;", 2)[0]) == "" {
			return FindingReference{}, true, fmt.Errorf("invalid redacted environment marker")
		}
		return FindingReference{}, true, nil
	default:
		return FindingReference{}, false, nil
	}
	if err != nil {
		return FindingReference{}, true, err
	}
	clean := filepath.ToSlash(filepath.Clean(path))
	if path == "" || filepath.IsAbs(path) || strings.Contains(path, "\\") || clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return FindingReference{}, true, fmt.Errorf("Finding reference path %q is not repository-contained", path)
	}
	return FindingReference{Path: clean, SHA256: digest}, true, nil
}

// ValidateFindingReference is for sources imported from a verified Finding,
// not arbitrary Hypothesis IDs. os.Root retains repository containment while
// permitting existing in-repository symlinks accepted by S7.
func ValidateFindingReference(root, ref string) error {
	source, supported, err := ParseFindingReference(ref)
	if err != nil {
		return err
	}
	if !supported {
		return fmt.Errorf("not an original typed Finding source: %q", ref)
	}
	if source.Path == "" {
		return nil
	}
	r, err := os.OpenRoot(root)
	if err != nil {
		return err
	}
	defer r.Close()
	f, err := r.Open(filepath.FromSlash(source.Path))
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("Finding source %s is not a regular file", source.Path)
	}
	hash := sha256.New()
	count, err := io.Copy(hash, f)
	if err != nil {
		return err
	}
	if hex.EncodeToString(hash.Sum(nil)) != source.SHA256 {
		return fmt.Errorf("Finding source %s SHA256 differs from the accepted reference", source.Path)
	}
	if strings.HasPrefix(ref, "command_output:") {
		output, _ := ParseCommandOutputReference(ref)
		if (!output.Truncated && output.Bytes != count) || (output.Truncated && output.Bytes <= count) {
			return fmt.Errorf("Finding command output byte count/truncation differs from the accepted reference")
		}
	}
	return nil
}
