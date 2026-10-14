package e2erun

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/fileview"
)

func testGit(t *testing.T, root string, args ...string) string {
	t.Helper()
	base := []string{"-C", root, "-c", "core.hooksPath=/dev/null", "-c", "commit.gpgsign=false", "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid"}
	cmd := exec.Command("git", append(base, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func fixtureProfile() Profile {
	tool := Tool{Path: "/missing/tool", SHA256: strings.Repeat("a", 64)}
	return Profile{SchemaVersion: "1.0.0", Platform: Platform{OS: "linux", Architecture: "amd64", KernelRelease: "fixture"}, Config: "tests/config.cjs", SourceRoots: []string{"tests"}, Node: tool, Modules: tool, Browsers: tool, System: tool, Isolator: tool, TimeoutSeconds: 1}
}

func testFormalView(t *testing.T, root string, profile Profile, extra ...fileview.Rule) (*fileview.View, FileDigest) {
	t.Helper()
	data, err := json.Marshal(profile)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, ProfilePath)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	testGit(t, root, "init", "-q", "-b", "development")
	if err := os.WriteFile(filepath.Join(root, ".git/info/exclude"), []byte(".claude/\n"), 0600); err != nil {
		t.Fatal(err)
	}
	testGit(t, root, "add", ".")
	testGit(t, root, "commit", "-qm", "Formal fixture")
	rules := append([]fileview.Rule{{Path: ".", Source: "git_tree"}}, extra...)
	view, err := fileview.New(root, "refs/heads/development", rules)
	if err != nil {
		t.Fatal(err)
	}
	return view, FileDigest{Path: ProfilePath, SHA256: digest(data)}
}

func TestFormalProfileRejectsDiskOverridesAndWrongCommit(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "tests"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "tests/config.cjs"), []byte("module.exports={};"), 0600); err != nil {
		t.Fatal(err)
	}
	profile := fixtureProfile()
	view, ref := testFormalView(t, root, profile)
	// Dirty local profile bytes cannot choose the executable or input set.
	if err := os.WriteFile(filepath.Join(root, ProfilePath), []byte(`{"node":{"path":"/bin/sh"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	loaded, err := ReadFormalProfile(view, ref)
	if err != nil || loaded.Node != profile.Node {
		t.Fatalf("formal profile unexpectedly used working tree: %+v, %v", loaded, err)
	}
	wrong := ref
	wrong.SHA256 = strings.Repeat("0", 64)
	if _, err := ReadFormalProfile(view, wrong); err == nil {
		t.Fatal("wrong profile hash accepted")
	}
	diskView, err := fileview.New(root, "refs/heads/development", []fileview.Rule{{Path: ".", Source: "git_tree"}, {Path: ProfilePath, Source: "disk"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ReadFormalProfile(diskView, ref); err == nil {
		t.Fatal("disk profile admitted")
	}
	override, err := fileview.New(root, "refs/heads/development", []fileview.Rule{{Path: ".", Source: "git_tree"}, {Path: "tests/config.cjs", Source: "disk"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := InspectInputs(context.Background(), formalInputs{override}, profile.SourceRoots); err == nil {
		t.Fatal("nested disk executable override admitted")
	}
	request := FormalRequest{Files: view, ProfileRef: ref, Mode: "collection", Binding: Binding{RuntimeID: "loop-formal", Generation: 1, Round: 1, SourceCommit: strings.Repeat("0", 40), SubjectDigest: strings.Repeat("a", 64)}}
	if _, err := RunFormal(context.Background(), request); err == nil {
		t.Fatal("binding different source commit accepted")
	}
	request.Binding.SourceCommit = view.Commit
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := RunFormal(ctx, request); !errors.Is(err, context.Canceled) {
		t.Fatalf("formal reads ignored their caller deadline: %v", err)
	}
	observation, err := RunFormal(context.Background(), request)
	if err == nil {
		t.Fatal("missing provisioned tools unexpectedly ran")
	}
	receipt, ok := observation.Receipt()
	if !ok || receipt.Invoked || receipt.Complete || receipt.Outcome != "unknown" || receipt.ExitCode != -1 || len(receipt.Problems) == 0 || receipt.ProfileRef == nil || *receipt.ProfileRef != ref {
		t.Fatalf("preparation failure lost its bound observation: %s", observation.Bytes())
	}
	if receipt.AttemptedAt == "" || receipt.StartedAt != "" || receipt.Stage != "preparation" {
		t.Fatalf("preparation falsely claimed an invocation: %+v", receipt)
	}
}

func TestToolFingerprintDescribesNormalizedCopiedTree(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "lib"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "lib/tool"), []byte("unchanged tool bytes"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "lib/tool"), filepath.Join(root, "absolute-link")); err != nil {
		t.Fatal(err)
	}
	copyPath := filepath.Join(t.TempDir(), "tool")
	first, err := copyTool(context.Background(), Tool{Path: root}, copyPath)
	if err != nil {
		t.Fatal(err)
	}
	second, err := InspectTool(context.Background(), copyPath)
	if err != nil || first != second.SHA256 {
		t.Fatalf("copy's normalized digest changed: %s / %+v / %v", first, second, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := InspectTool(ctx, filepath.Join(root, "lib/tool")); err == nil {
		t.Fatal("cancelled single-file fingerprint ignored deadline")
	}
}

func TestProfileRejectsUnknownAndTrailingObjects(t *testing.T) {
	data, _ := json.Marshal(fixtureProfile())
	if _, err := DecodeProfile(data); err != nil {
		t.Fatal(err)
	}
	for _, bad := range [][]byte{append(append([]byte(nil), data...), []byte(" {}")...), []byte(strings.Replace(string(data), `"schema_version"`, `"unknown":1,"schema_version"`, 1))} {
		if _, err := DecodeProfile(bad); err == nil {
			t.Fatal("ambiguous/unknown profile accepted")
		}
	}
}
