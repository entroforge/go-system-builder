package semantic

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBundledAgentDefinitionsHavePlatformMetadata(t *testing.T) {
	root := filepath.Join("..", "..")
	for _, role := range []string{"backend-builder", "frontend-builder", "test-builder", "delivery-verifier", "document-verifier", "e2e-tester", "qa", "investigator"} {
		if _, err := os.Stat(filepath.Join(root, "agents", role+".md")); err != nil {
			t.Fatal(err)
		}
	}
	if err := ValidateAgentDefinitions(root); err != nil {
		t.Fatal(err)
	}
}
func TestAgentDefinitionCheckPrefersInstalledAndRejectsBrokenMetadata(t *testing.T) {
	for _, tc := range []struct {
		body string
		ok   bool
	}{
		{"# Investigator\n", false}, {"---\nname: investigator\ndescription: causal investigation\n---\n# Investigator", true},
		{"---\nname: qa\ndescription: x\n---\n", false}, {"---\nname: investigator\n---\n", false},
		{"---\nname: investigator\ndescription: x\n", false}, {"---\nname: investigator\nname: qa\ndescription: x\n---", false},
	} {
		t.Run(tc.body, func(t *testing.T) {
			root := t.TempDir()
			dir := filepath.Join(root, ".claude", "agents")
			if err := os.MkdirAll(dir, 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "investigator.md"), []byte(tc.body), 0644); err != nil {
				t.Fatal(err)
			}
			if err := ValidateAgentDefinitions(root); (err == nil) != tc.ok {
				t.Fatalf("got %v want ok=%v", err, tc.ok)
			}
		})
	}
}
