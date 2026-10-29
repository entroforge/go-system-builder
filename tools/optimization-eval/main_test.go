package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCatalogContainsTwelveExplicitFamiliesAndNoPublicOracle(t *testing.T) {
	catalog, err := loadCatalog()
	if err != nil || len(catalog) != 12 {
		t.Fatal(catalog, err)
	}
	for _, c := range catalog {
		data, _ := json.Marshal(publicView(c))
		if strings.Contains(string(data), c.Oracle.Test) || strings.Contains(string(data), "mechanism_oracle") || strings.Contains(string(data), "fixture_refs") {
			t.Fatal("oracle leaked into public view")
		}
		encoded, _ := json.Marshal(c)
		if _, err := decodeCase(append(encoded, []byte(` {}`)...)); err == nil {
			t.Fatal("multiple case objects accepted")
		}
		c.PublicRefs = []string{"../private.json"}
		encoded, _ = json.Marshal(c)
		if _, err := decodeCase(encoded); err == nil {
			t.Fatal("case escaped source root")
		}
	}
}

func TestExactOriginalTestRunRequiredBeforeCountingSuccess(t *testing.T) {
	expected := target{"./internal/cli", "TestExample/sub"}
	events := func(rows ...testEvent) []byte {
		var out bytes.Buffer
		for _, event := range rows {
			json.NewEncoder(&out).Encode(event)
		}
		return out.Bytes()
	}
	pkg := "github.com/entroforge/go-system-builder/internal/cli"
	for _, tc := range []struct {
		events []byte
		status string
	}{
		{events(testEvent{Action: "pass", Package: pkg}), "error"},
		{events(testEvent{Action: "run", Package: pkg, Test: "TestExample"}, testEvent{Action: "pass", Package: pkg, Test: "TestExample"}), "error"},
		{events(testEvent{Action: "run", Package: pkg, Test: expected.Test}, testEvent{Action: "pass", Package: pkg, Test: expected.Test}), "pass"},
		{events(testEvent{Action: "run", Package: pkg, Test: expected.Test}, testEvent{Action: "skip", Package: pkg, Test: expected.Test}), "skip"},
		{events(testEvent{Action: "run", Package: pkg, Test: expected.Test}, testEvent{Action: "skip", Package: pkg, Test: expected.Test + "/required"}, testEvent{Action: "pass", Package: pkg, Test: expected.Test}), "skip"},
		{events(testEvent{Action: "run", Package: pkg, Test: expected.Test}, testEvent{Action: "pass", Package: pkg, Test: expected.Test}, testEvent{Action: "fail", Package: pkg}), "fail"},
		{[]byte("not JSON\n"), "error"},
	} {
		status, _ := evaluateEvents(tc.events, expected)
		if status != tc.status {
			t.Fatal(status, tc.status)
		}
	}
}

func TestFrozenDirtySourcePreservesOriginalAndDetectsMutation(t *testing.T) {
	root := t.TempDir()
	snapshot := filepath.Join(t.TempDir(), "source")
	os.Mkdir(snapshot, 0700)
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatal(err, string(out))
		}
	}
	os.MkdirAll(filepath.Join(root, "docs/control"), 0700)
	os.WriteFile(filepath.Join(root, "go.mod"), []byte("module fixture\n"), 0600)
	os.WriteFile(filepath.Join(root, "docs/control/loop-definition.json"), []byte("{}"), 0600)
	git("init", "-b", "fixture")
	git("config", "user.name", "Fixture")
	git("config", "user.email", "fixture@example.invalid")
	git("add", ".")
	git("commit", "-qm", "base")
	dirty := []byte("{\"actual_dirty_input\":true}")
	os.WriteFile(filepath.Join(root, "docs/control/loop-definition.json"), dirty, 0600)
	os.MkdirAll(filepath.Join(root, ".claude"), 0700)
	os.WriteFile(filepath.Join(root, ".claude/loop-state.json"), []byte("live"), 0600)
	os.WriteFile(filepath.Join(root, "docs/.env"), []byte("private"), 0600)
	m, err := frozenSnapshot(root, snapshot)
	if err != nil || !m.Dirty {
		t.Fatal(m, err)
	}
	actual, _ := os.ReadFile(filepath.Join(snapshot, "docs/control/loop-definition.json"))
	if !bytes.Equal(actual, dirty) {
		t.Fatal("dirty input was lost")
	}
	if _, err := os.Stat(filepath.Join(snapshot, ".claude")); !os.IsNotExist(err) {
		t.Fatal("live authority copied")
	}
	if _, err := os.Stat(filepath.Join(snapshot, "docs/.env")); !os.IsNotExist(err) {
		t.Fatal("private env copied")
	}
	if err := verifySnapshot(snapshot, m); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(snapshot, "go.mod"), []byte("mutated"), 0600)
	if err := verifySnapshot(snapshot, m); err == nil {
		t.Fatal("source mutation not detected")
	}
	original, _ := os.ReadFile(filepath.Join(root, "docs/control/loop-definition.json"))
	if !bytes.Equal(original, dirty) {
		t.Fatal("original project changed")
	}
}

func TestHostUnavailableAndEvidenceLimitCannotBecomePass(t *testing.T) {
	catalog, err := loadCatalog()
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(t.TempDir(), "case")
	r := executeCase(context.Background(), t.TempDir(), dir, "real_host", catalog[0])
	if r.Status != "not_run" || r.Usage.Available || r.ReadSkillTrace.Available || len(r.Argv) != 0 {
		t.Fatal("unavailable host was fabricated", r)
	}
	var out bytes.Buffer
	cancelled := false
	w := boundedLog{&out, 3, func() { cancelled = true }}
	if _, err := w.Write([]byte("oversized")); err == nil || !cancelled {
		t.Fatal("unbounded evidence was accepted")
	}
	if out.Len() != 0 {
		t.Fatal("partial overflowing log wrote a misleading fragment")
	}
}
