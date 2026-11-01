package repair

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
)

// ComputeChangeset records the paths provided by the caller or observed in an
// actual Git diff. Content digests and Git object IDs are not recorded.
func ComputeChangeset(root string, request ChangesetRequest) (Changeset, error) {
	var paths []string
	statuses := map[string]string{}
	source := "explicit"
	if len(request.ExplicitPaths) > 0 {
		paths = append(paths, request.ExplicitPaths...)
	} else {
		if strings.TrimSpace(request.BaseRef) == "" || strings.TrimSpace(request.HeadRef) == "" {
			return Changeset{}, errors.New("changeset requires explicit paths or both base_ref and head_ref")
		}
		output, err := exec.Command("git", "-C", root, "diff", "--name-status", "--diff-filter=ACMRTUXBD", request.BaseRef+".."+request.HeadRef).CombinedOutput()
		if err != nil {
			return Changeset{}, fmt.Errorf("git diff changeset: %w: %s", err, strings.TrimSpace(string(output)))
		}
		for _, line := range strings.Split(string(output), "\n") {
			fields := strings.Split(line, "\t")
			if len(fields) < 2 || strings.TrimSpace(fields[1]) == "" {
				continue
			}
			status := fields[0]
			path := fields[1]
			if strings.HasPrefix(status, "R") || strings.HasPrefix(status, "C") {
				if len(fields) < 3 {
					continue
				}
				path = fields[2]
				status = "modified"
			} else if len(status) > 1 {
				status = status[:1]
			}
			if status == "A" {
				status = "added"
			} else if status == "D" {
				status = "deleted"
			} else {
				status = "modified"
			}
			paths = append(paths, path)
			statuses[path] = status
		}
		source = "git_diff"
	}
	paths, err := normalizedPaths(paths)
	if err != nil {
		return Changeset{}, err
	}
	artifacts := make([]ArtifactRef, 0, len(paths))
	for _, path := range paths {
		if source == "explicit" {
			absolute, pathErr := repositoryPath(root, path)
			if pathErr != nil {
				return Changeset{}, pathErr
			}
			info, statErr := os.Stat(absolute)
			if statErr != nil {
				return Changeset{}, fmt.Errorf("inspect changed artifact %s: %w", path, statErr)
			}
			if !info.Mode().IsRegular() {
				return Changeset{}, fmt.Errorf("changed artifact %s is not a regular file", path)
			}
		}
		status := statuses[path]
		if status == "" {
			status = "modified"
		}
		artifacts = append(artifacts, ArtifactRef{ID: "changed-" + strings.NewReplacer("/", "-", "\\", "-", ".", "-").Replace(path), Path: path, Status: status})
	}
	changeset := Changeset{
		SchemaVersion: "1.0.0", RecordType: "repair_changeset",
		SessionID: request.SessionID, Source: source,
		Artifacts: artifacts, ComputedAt: nowOr(request.OccurredAt),
	}
	changeset.ChangesetID = repairChangesetID(changeset)
	return changeset, nil
}

func repairChangesetID(changeset Changeset) string {
	// The event's existing session, source and creation time identify this
	// observation; no source content or Git object ID is encoded or hashed.
	coordinates, _ := json.Marshal([]string{changeset.SessionID, changeset.Source, changeset.ComputedAt})
	return "repair-changeset-" + base64.RawURLEncoding.EncodeToString(coordinates)
}

func PersistChangeset(root string, changeset Changeset) (ArtifactRef, error) {
	// Do not copy old machine metadata or Git object coordinates into a new
	// record. The caller's original value and any historical artifact remain
	// unchanged.
	changeset.Artifacts = append([]ArtifactRef{}, changeset.Artifacts...)
	baseID := changeset.ChangesetID
	for attempt := 0; ; attempt++ {
		if attempt > 0 {
			changeset.ChangesetID = baseID + "-event-" + strconv.Itoa(attempt)
		}
		path := responsibilityArtifactPath("changesets", changeset.ChangesetID)
		ref, err := writeImmutable(root, path, "repair-changeset.schema.json", changeset)
		if err == nil {
			if attempt > 0 {
				ref.ID = changeset.ChangesetID
			}
			return ref, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return ref, err
		}
		// Re-observing the same original event must not create another record.
		// Compare only its professional path/status projection; an unrelated or
		// unreadable existing file is preserved and is not a reverse gate.
		previous, readErr := ValidateChangeset(root, ArtifactRef{Path: path})
		if readErr == nil && previous.ChangesetID == changeset.ChangesetID && previous.SessionID == changeset.SessionID &&
			previous.Source == changeset.Source && previous.ComputedAt == changeset.ComputedAt &&
			reflect.DeepEqual(previous.Artifacts, changeset.Artifacts) {
			ref := ArtifactRef{Path: path}
			if attempt > 0 {
				ref.ID = changeset.ChangesetID
			}
			return ref, nil
		}
	}
}

func ValidateChangeset(root string, ref ArtifactRef) (Changeset, error) {
	var changeset Changeset
	data, err := readArtifact(root, ref, "repair-changeset.schema.json")
	if err != nil {
		return Changeset{}, err
	}
	if err := json.Unmarshal(data, &changeset); err != nil {
		return Changeset{}, fmt.Errorf("decode artifact %s: %w", ref.Path, err)
	}
	if changeset.RecordType != "repair_changeset" {
		return Changeset{}, fmt.Errorf("artifact %s is not a repair changeset", ref.Path)
	}
	return changeset, nil
}

func normalizedPaths(paths []string) ([]string, error) {
	seen := map[string]struct{}{}
	result := make([]string, 0, len(paths))
	for _, raw := range paths {
		if filepath.IsAbs(raw) {
			return nil, fmt.Errorf("changed path %q must be repository-relative", raw)
		}
		clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(strings.TrimSpace(raw))))
		if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || clean == "" {
			return nil, fmt.Errorf("changed path %q is not a repository-relative file", raw)
		}
		if _, exists := seen[clean]; !exists {
			seen[clean] = struct{}{}
			result = append(result, clean)
		}
	}
	sort.Strings(result)
	return result, nil
}
