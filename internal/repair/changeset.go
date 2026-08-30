package repair

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// ComputeChangeset fingerprints either an explicit list of changed paths or a
// Git two-revision diff. It does not mutate Git, Runtime, or any artifact.
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
	if len(paths) == 0 {
		return Changeset{}, errors.New("changeset contains no changed artifacts")
	}
	artifacts := make([]ArtifactRef, 0, len(paths))
	for _, path := range paths {
		var data []byte
		if source == "git_diff" {
			data, err = gitBlob(root, request.HeadRef, path)
			if statuses[path] == "deleted" {
				data, err = gitBlob(root, request.BaseRef, path)
			}
		} else {
			absolute, pathErr := repositoryPath(root, path)
			if pathErr == nil {
				data, err = os.ReadFile(absolute)
			} else {
				err = pathErr
			}
		}
		if err != nil {
			return Changeset{}, fmt.Errorf("fingerprint changed artifact %s: %w", path, err)
		}
		status := statuses[path]
		if status == "" {
			status = "modified"
		}
		artifacts = append(artifacts, ArtifactRef{ID: "changed-" + strings.NewReplacer("/", "-", "\\", "-", ".", "-").Replace(path), Path: path, SHA256: sha256Bytes(data), Status: status})
	}
	lines := make([]string, 0, len(artifacts))
	for _, artifact := range artifacts {
		lines = append(lines, artifact.Path+":"+artifact.SHA256)
	}
	sort.Strings(lines)
	digest := sha256Bytes([]byte(strings.Join(lines, "\n")))
	return Changeset{
		SchemaVersion: "1.0.0", RecordType: "repair_changeset", ChangesetID: "repair-changeset-" + digest[:16],
		SessionID: request.SessionID, Source: source, BaseRef: request.BaseRef, HeadRef: request.HeadRef,
		Artifacts: artifacts, Digest: digest, ComputedAt: nowOr(request.OccurredAt),
	}, nil
}

func gitBlob(root, revision, path string) ([]byte, error) {
	if strings.TrimSpace(revision) == "" {
		return nil, errors.New("git blob revision is empty")
	}
	output, err := exec.Command("git", "-C", root, "show", revision+":"+path).CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("git show %s:%s: %w: %s", revision, path, err, strings.TrimSpace(string(output)))
	}
	return output, nil
}

func PersistChangeset(root string, changeset Changeset) (ArtifactRef, error) {
	return writeImmutable(root, artifactRoot+"/changesets/"+changeset.ChangesetID+".json", "repair-changeset.schema.json", changeset)
}

func ValidateChangeset(root string, ref ArtifactRef) (Changeset, error) {
	var changeset Changeset
	if err := decodeArtifact(root, ref, "repair-changeset.schema.json", &changeset); err != nil {
		return Changeset{}, err
	}
	if changeset.RecordType != "repair_changeset" || len(changeset.Artifacts) == 0 {
		return Changeset{}, fmt.Errorf("artifact %s is not a non-empty repair changeset", ref.Path)
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
