package main

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"path"
	"regexp"
	"sort"
	"strings"
)

//go:embed cases/*/case.json
var catalogFiles embed.FS

type target struct {
	Package string `json:"package"`
	Test    string `json:"test"`
}
type evalCase struct {
	ID                string            `json:"id"`
	Version           string            `json:"version"`
	Family            string            `json:"family"`
	Role              string            `json:"role"`
	PublicInput       string            `json:"public_input"`
	PublicRefs        []string          `json:"public_refs"`
	FixtureRefs       []string          `json:"fixture_refs"`
	Oracle            target            `json:"mechanism_oracle"`
	Variables         map[string]string `json:"variables"`
	ExternalResources []string          `json:"external_resources"`
	TimeoutSeconds    int               `json:"timeout_seconds"`
	LegalEnd          string            `json:"legal_end"`
	ProofScope        string            `json:"proof_scope"`
}

var validID = regexp.MustCompile(`^G08-[0-9]{2}$`)
var validTest = regexp.MustCompile(`^Test[A-Za-z0-9_]+(?:/[A-Za-z0-9_]+)*$`)

func decodeCase(data []byte) (evalCase, error) {
	var c evalCase
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(&c); err != nil {
		return c, err
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		return c, fmt.Errorf("case must contain one JSON object")
	}
	if !validID.MatchString(c.ID) || c.Version != "1.0.0" || c.Family == "" || c.Role == "" || c.PublicInput == "" || c.LegalEnd == "" || c.ProofScope == "" {
		return c, fmt.Errorf("case identity, version and explicit input/end/proof scope required")
	}
	if c.TimeoutSeconds < 1 || c.TimeoutSeconds > 600 || c.Oracle.Package != "./internal/cli" || !validTest.MatchString(c.Oracle.Test) {
		return c, fmt.Errorf("case requires a bounded original CLI Test/subtest target")
	}
	if len(c.PublicRefs) == 0 || len(c.FixtureRefs) == 0 || c.Variables == nil || c.ExternalResources == nil {
		return c, fmt.Errorf("case sources, variables and external resources must be explicit")
	}
	for _, ref := range append(append([]string{}, c.PublicRefs...), c.FixtureRefs...) {
		if !safeRelative(ref) {
			return c, fmt.Errorf("unsafe case source %q", ref)
		}
	}
	return c, nil
}

func safeRelative(ref string) bool {
	if ref == "" || strings.ContainsAny(ref, "\\\x00") || strings.HasPrefix(ref, "/") || path.Clean(ref) != ref {
		return false
	}
	for _, component := range strings.Split(ref, "/") {
		if component == ".." || component == "." || strings.HasPrefix(component, ".env") || component == ".git" || component == ".claude" || component == ".local" || component == ".worktrees" {
			return false
		}
	}
	return true
}

func loadCatalog() ([]evalCase, error) {
	paths, err := fs.Glob(catalogFiles, "cases/*/case.json")
	if err != nil {
		return nil, err
	}
	result := []evalCase{}
	seen := map[string]bool{}
	for _, p := range paths {
		data, err := catalogFiles.ReadFile(p)
		if err != nil {
			return nil, err
		}
		c, err := decodeCase(data)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", p, err)
		}
		if seen[c.ID] {
			return nil, fmt.Errorf("duplicate case %s", c.ID)
		}
		seen[c.ID] = true
		result = append(result, c)
	}
	if len(result) != 12 {
		return nil, fmt.Errorf("catalog requires all twelve initial families")
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result, nil
}

// Public views deliberately omit test oracle/fixture paths. Mechanism runs
// have no model prompt; host mode remains not_run until a real adapter exists.
func publicView(c evalCase) any {
	return struct {
		ID      string   `json:"id"`
		Version string   `json:"version"`
		Role    string   `json:"role"`
		Input   string   `json:"input"`
		Refs    []string `json:"refs"`
	}{c.ID, c.Version, c.Role, c.PublicInput, c.PublicRefs}
}
