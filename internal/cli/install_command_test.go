package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestInstallRefusesExistingTargetWithoutMutation(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "keep.txt")
	os.WriteFile(path, []byte("unchanged"), 0644)
	var out, err bytes.Buffer
	if runInstall([]string{"--source", t.TempDir(), "--root", root}, &out, &err) == 0 {
		t.Fatal("nonempty target accepted")
	}
	got, _ := os.ReadFile(path)
	if string(got) != "unchanged" {
		t.Fatal("target changed")
	}
	entries, _ := os.ReadDir(root)
	if len(entries) != 1 {
		t.Fatal("target gained files")
	}
}
func TestInstallInvalidSourceDoesNotPublishRuntime(t *testing.T) {
	root := filepath.Join(t.TempDir(), "project")
	var out, err bytes.Buffer
	if runInstall([]string{"--source", t.TempDir(), "--root", root}, &out, &err) == 0 {
		t.Fatal("invalid release accepted")
	}
	if _, e := os.Stat(root); !os.IsNotExist(e) {
		t.Fatalf("invalid source published target: %v", e)
	}
}

func TestLegacyLayoutCannotBeBypassedByHelpValue(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "docs/product/requirements"), 0755)
	os.WriteFile(filepath.Join(root, "docs/product/requirements/REQ-old.md"), []byte("legacy"), 0644)
	for _, rootFlag := range []string{"--root=", "-root="} {
		var out, err bytes.Buffer
		code := Run([]string{"req", "bind", rootFlag + root, "--approved-by", "--help"}, bytes.NewReader(nil), &out, &err)
		if code == 0 || !bytes.Contains(err.Bytes(), []byte("layout migration required")) {
			t.Fatalf("legacy bypass: code=%d err=%s", code, err.String())
		}
		if _, e := os.Stat(filepath.Join(root, ".claude")); !os.IsNotExist(e) {
			t.Fatal("legacy check wrote Runtime")
		}
	}
}

func TestPendingInitCannotReplayLegacyState(t *testing.T) {
	for _, fresh := range []string{`{"definition":{"path":"docs/loop-definition.json"}}`, `{"definition":{"path":"docs/control/loop-definition.json"},"bound_req":{"path":"docs/product/requirements/REQ-001.md"}}`} {
		t.Run(fresh, func(t *testing.T) {
			root := t.TempDir()
			dir := filepath.Join(root, ".claude")
			os.MkdirAll(dir, 0755)
			marker := filepath.Join(dir, "loop-init-pending.json")
			data := []byte(`{"schema_version":"1.0.0","fresh_state":` + fresh + `}`)
			os.WriteFile(marker, data, 0644)
			var out, err bytes.Buffer
			if code := Run([]string{"init", "--root", root}, bytes.NewReader(nil), &out, &err); code == 0 || !bytes.Contains(err.Bytes(), []byte("layout migration required")) {
				t.Fatalf("legacy init replay: %d %s", code, err.String())
			}
			after, _ := os.ReadFile(marker)
			if !bytes.Equal(after, data) {
				t.Fatal("marker changed")
			}
			if _, e := os.Stat(filepath.Join(dir, "loop-state.json")); !os.IsNotExist(e) {
				t.Fatal("legacy pending state published")
			}
		})
	}
}

func TestS10ProjectionUsesInstalledCanonicalPaths(t *testing.T) {
	expected := []string{"docs/reports/acceptance/ACC-template.md", "docs/reports/release-audits/TEMPLATE.md", "docs/rules/release-architecture-audit.md", ".claude/skills/acceptance-and-handoff/SKILL.md"}
	read := projectionContracts["S10"].Read
	for _, path := range expected {
		found := false
		for _, got := range read {
			if got == path {
				found = true
			}
		}
		if !found {
			t.Fatalf("missing S10 read target %s: %v", path, read)
		}
		source := path
		if len(source) > 8 && source[:8] == ".claude/" {
			source = source[8:]
		}
		if _, err := os.Stat(filepath.Join("../..", source)); err != nil {
			t.Fatalf("S10 source target missing: %s: %v", source, err)
		}
	}
}

func TestReleaseManifestRejectsCorruptMissingAndExtraFiles(t *testing.T) {
	for _, mode := range []string{"intact", "corrupt", "missing", "extra", "symlink"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			p := filepath.Join(root, "asset.txt")
			if err := os.WriteFile(p, []byte("asset"), 0644); err != nil {
				t.Fatal(err)
			}
			files, err := installInventory(root, packageManifest)
			if err != nil {
				t.Fatal(err)
			}
			data, err := json.Marshal(map[string]any{"schema_version": 1, "files": files})
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, packageManifest), data, 0644); err != nil {
				t.Fatal(err)
			}
			switch mode {
			case "corrupt":
				err = os.WriteFile(p, []byte("changed"), 0644)
			case "missing":
				err = os.Remove(p)
			case "extra":
				err = os.WriteFile(filepath.Join(root, "extra"), []byte("extra"), 0644)
			case "symlink":
				if err = os.Remove(p); err == nil {
					err = os.Symlink(filepath.Join(root, packageManifest), p)
				}
				if err != nil {
					t.Skipf("symlinks unavailable: %v", err)
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			err = verifyInstallManifest(root)
			if (mode == "intact") != (err == nil) {
				t.Fatalf("%s validation: %v", mode, err)
			}
		})
	}
}
