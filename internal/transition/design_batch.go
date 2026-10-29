package transition

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/entroforge/go-system-builder/internal/docscope"
	"github.com/entroforge/go-system-builder/internal/fileview"
	"github.com/entroforge/go-system-builder/internal/projectlayout"
)

// RegistrationDocument is a member of the original document-registration
// batch. Reading the batch does not register or validate its completion.
type RegistrationDocument struct {
	ID          string
	Path        string
	Status      string
	LegacyOwner bool
	data        []byte
}

// registrationDocument is shared by the action and its read-only guidance.
// Filename, ownership and status exclusions must not drift between them.
func registrationDocument(files fileview.Reader, entry os.DirEntry, dirRel string, prefixes []string, boundID string) (RegistrationDocument, bool, error) {
	name := entry.Name()
	id := strings.TrimSuffix(name, ".md")
	if entry.IsDir() || !strings.HasSuffix(name, ".md") ||
		strings.Contains(strings.ToLower(name), "template") ||
		strings.EqualFold(name, "README.md") || !hasAnyPrefix(id, prefixes) {
		return RegistrationDocument{}, false, nil
	}
	rel := filepath.ToSlash(filepath.Join(dirRel, name))
	data, err := files.ReadFile(rel)
	if err != nil {
		return RegistrationDocument{}, false, fmt.Errorf("read %s: %w", rel, err)
	}
	if !docscope.Belongs(data, boundID) {
		return RegistrationDocument{}, false, nil
	}
	status := ParseMarkdownField(string(data), "状态", "Status")
	// These exclusions are the existing action's membership semantics.
	if status == "" || status == "cancelled" {
		return RegistrationDocument{}, false, nil
	}
	return RegistrationDocument{ID: id, Path: rel, Status: status, LegacyOwner: len(docscope.Owners(data)) == 0, data: data}, true, nil
}

// DesignRegistrationBatch projects the same disk-authoritative membership as
// PTR-PLAN-01. It intentionally performs no Git closure construction or Gate.
func DesignRegistrationBatch(root string, state map[string]any) ([]RegistrationDocument, error) {
	if root == "" {
		root = "."
	}
	files := fileview.Disk{Root: root}
	entries, err := files.ReadDir(projectlayout.Architecture)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", projectlayout.Architecture, err)
	}
	bound, _ := state["bound_req"].(map[string]any)
	boundID, _ := bound["id"].(string)
	var batch []RegistrationDocument
	for _, entry := range entries {
		doc, member, err := registrationDocument(files, entry, projectlayout.Architecture, []string{"ARCHITECTURE-"}, boundID)
		if err != nil {
			return batch, err
		}
		if member {
			batch = append(batch, doc)
		}
	}
	return batch, nil
}
