package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/entroforge/go-system-builder/internal/integration"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

// Preserve Worker-authored lifecycle envelopes before their worktree can be
// cleaned up. Content-addressed envelopes survive retries and failed CAS;
// only the subsequent domain command may register them as authority.
func freezeWorkerSubmission(main, worker, path string, e workspace.Execution) (string, error) {
	physical, err := workspace.Canonical(path)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(worker, physical)
	if err != nil || !filepath.IsLocal(rel) {
		// Existing Main artifacts are already durable. Do not import an
		// arbitrary external file or another Worker's private submission.
		rel, err = filepath.Rel(main, physical)
		if err != nil || !filepath.IsLocal(rel) {
			return "", fmt.Errorf("submission escapes the bound project")
		}
		_, marked, err := workspace.PointerRoot(filepath.Dir(physical))
		if err != nil {
			return "", err
		}
		if marked {
			return "", fmt.Errorf("submission belongs to another Worker")
		}
		return physical, nil
	}
	info, err := os.Stat(physical)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() || info.Size() > 8<<20 {
		return "", fmt.Errorf("Worker lifecycle submission must be a JSON file no larger than 8 MiB")
	}
	data, err := os.ReadFile(physical)
	if err != nil {
		return "", err
	}
	if !json.Valid(data) {
		return "", fmt.Errorf("Worker lifecycle submission is not valid JSON")
	}
	h := sha256.Sum256(data)
	dir := filepath.Join(filepath.Dir(integration.CheckpointPath(main, e.RuntimeID, e.BaselineGeneration, e.AssignmentID)), "submissions")
	if err = os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	dest := filepath.Join(dir, hex.EncodeToString(h[:])+".json")
	if existing, err := os.ReadFile(dest); err == nil {
		if sha256.Sum256(existing) != h {
			return "", fmt.Errorf("durable submission hash drift")
		}
		return dest, nil
	} else if !os.IsNotExist(err) {
		return "", err
	}
	// Publish a fully written file with a hard link: concurrent identical
	// submissions cannot expose partial bytes or overwrite another envelope.
	f, err := os.CreateTemp(dir, ".submission-*")
	if err != nil {
		return "", err
	}
	defer os.Remove(f.Name())
	_, err = f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return "", err
	}
	if closeErr != nil {
		return "", closeErr
	}
	if err = os.Link(f.Name(), dest); err != nil && !os.IsExist(err) {
		return "", err
	}
	existing, err := os.ReadFile(dest)
	if err != nil {
		return "", err
	}
	if sha256.Sum256(existing) != h {
		return "", fmt.Errorf("durable submission collision")
	}
	return dest, nil
}
