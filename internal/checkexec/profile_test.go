package checkexec

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func testProfile() Profile {
	return Profile{ID: "CP-LOCAL", Version: "v1", CheckIndex: 0, Command: "true", Location: "main", TimeoutSeconds: 1, EnvironmentPolicy: "declared_only", Environment: []EnvFact{}, Dependencies: []FileRef{}, Fixtures: []FileRef{}, InputClosure: "whole_tree", SideEffects: "none", ReusePolicy: "never"}
}
func TestProfileRequiresExactOriginalCommandAndPolicy(t *testing.T) {
	p := testProfile()
	if err := ValidateProfiles([]Profile{p}, []string{"true"}, "main"); err != nil {
		t.Fatal(err)
	}
	p.Command = "false"
	if err := ValidateProfiles([]Profile{p}, []string{"true"}, "main"); err == nil {
		t.Fatal("profile changed original command")
	}
	p = testProfile()
	if err := ValidateProfiles([]Profile{p}, []string{"true"}, "worker"); err == nil {
		t.Fatal("profile changed original runner")
	}
	p.Environment = []EnvFact{{Name: "SECRET", Value: nil}}
	if err := ValidateProfiles([]Profile{p}, []string{"true"}, "main"); err == nil {
		t.Fatal("profile dumped unapproved environment")
	}
	p = testProfile()
	p.EnvironmentPolicy = "inherited"
	p.ReusePolicy = "same_input_same_responsibility"
	if err := ValidateProfiles([]Profile{p}, []string{"true"}, "main"); err == nil {
		t.Fatal("unknown inherited env became reusable")
	}
}

func TestEnabledProfileRequiresClosedOriginalWorkerCheck(t *testing.T) {
	p := testProfile()
	p.Location = "worker"
	p.ReuseMode = "enabled"
	p.ReusePolicy = "same_input_same_responsibility"
	p.Command = "test -f src/product.go"
	if err := ValidateProfiles([]Profile{p}, []string{p.Command}, "worker"); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"main", "never", "external", "shell", "inherited", "side_effects"} {
		q := p
		switch kind {
		case "main":
			q.Location = "main"
		case "never":
			q.ReusePolicy = "never"
		case "external":
			q.Command = "go test ./..."
		case "shell":
			q.Command = "test -f src/product.go && true"
		case "inherited":
			q.EnvironmentPolicy = "inherited"
		case "side_effects":
			q.SideEffects = "unknown"
		}
		if err := ValidateProfiles([]Profile{q}, []string{q.Command}, q.Location); err == nil {
			t.Fatal("unclosed enabled profile admitted", kind)
		}
	}
}
func TestProfileObservesContentRunnerEnvironmentWithoutReuse(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "fixture.txt")
	os.WriteFile(path, []byte("fixture"), 0600)
	p := testProfile()
	p.Fixtures = []FileRef{{Path: "fixture.txt", SHA256: Hash([]byte("fixture"))}}
	first, err := ObserveProfile(context.Background(), root, p)
	if err != nil {
		t.Fatal(err)
	}
	if first.ReuseEligible || first.SubjectDigest == "" || first.EnvironmentDigest == "" || first.RunnerDigest == "" || first.ProfileDigest != p.Digest() {
		t.Fatal("observation guessed reuse", first)
	}
	os.MkdirAll(filepath.Join(root, ".git"), 0700)
	os.WriteFile(filepath.Join(root, ".git/unrelated"), []byte("head movement"), 0600)
	same, err := ObserveProfile(context.Background(), root, p)
	if err != nil || same.SubjectDigest != first.SubjectDigest {
		t.Fatal("irrelevant Git metadata became tested content")
	}
	os.WriteFile(filepath.Join(root, "owned.txt"), []byte("new content"), 0600)
	changed, err := ObserveProfile(context.Background(), root, p)
	if err != nil || changed.SubjectDigest == first.SubjectDigest {
		t.Fatal("tested content drift ignored")
	}
	os.WriteFile(path, []byte("changed fixture"), 0600)
	if _, err := ObserveProfile(context.Background(), root, p); err == nil {
		t.Fatal("fixture version drift ignored")
	}
}
func TestProfileUnsetAndDeclaredOnlyEnvAreDistinct(t *testing.T) {
	t.Setenv("CI", "")
	p := testProfile()
	p.Environment = []EnvFact{{Name: "CI", Value: nil}}
	if _, err := p.CommandEnvironment(); err == nil {
		t.Fatal("empty environment treated as unset")
	}
	value := ""
	p.Environment[0].Value = &value
	env, err := p.CommandEnvironment()
	if err != nil || len(env) != 1 || env[0] != "CI=" {
		t.Fatal("declared environment inherited arbitrary fields", env, err)
	}
}
