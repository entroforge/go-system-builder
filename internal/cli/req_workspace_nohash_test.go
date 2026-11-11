package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestREQWorkspaceBindsOriginalBranchWithoutDigest(t *testing.T) {
	for _, digest := range []string{"", "wrong-old-value"} {
		t.Run(digest, func(t *testing.T) {
			fix := newRuntimeFixture(t)
			for _, args := range [][]string{{"init", "-b", "dev"}, {"config", "user.name", "Test"}, {"config", "user.email", "test@example.invalid"}} {
				if out, err := runGit(t, fix.root, args...); err != nil {
					t.Fatalf("git %v: %s: %v", args, out, err)
				}
			}
			bound := fix.state["bound_req"].(map[string]any)
			if digest == "" {
				delete(bound, "sha256")
			} else {
				bound["sha256"] = digest
			}
			reqPath := bound["path"].(string)
			full := filepath.Join(fix.root, reqPath)
			if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(full, []byte("# Original REQ\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			for _, args := range [][]string{{"add", reqPath}, {"commit", "-m", "original requirement"}} {
				if out, err := runGit(t, fix.root, args...); err != nil {
					t.Fatalf("git %v: %s: %v", args, out, err)
				}
			}
			fix.persist(t)
			var out, errOut bytes.Buffer
			if code := Run([]string{"req", "workspace", "--root", fix.root, "--dev-branch", "dev", "--release-upstream", "release"}, nil, &out, &errOut); code != 0 {
				t.Fatalf("req workspace failed: code=%d stderr=%s", code, errOut.String())
			}
			var result map[string]any
			if err := json.Unmarshal(out.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			workspace := result["workspace"].(map[string]any)
			if workspace["dev_branch"] != "dev" || workspace["project_root"] != fix.root {
				t.Fatalf("actual branch binding lost: %v", workspace)
			}
			if _, exists := workspace["bound_commit"]; exists {
				t.Fatalf("new binding registered a Git OID: %v", workspace)
			}
		})
	}
}

func TestREQWorkspaceReportsMissingOriginalREQ(t *testing.T) {
	fix := newRuntimeFixture(t)
	for _, args := range [][]string{{"init", "-b", "dev"}, {"config", "user.name", "Test"}, {"config", "user.email", "test@example.invalid"}, {"commit", "--allow-empty", "-m", "base"}} {
		if out, err := runGit(t, fix.root, args...); err != nil {
			t.Fatalf("git %v: %s: %v", args, out, err)
		}
	}
	fix.persist(t)
	var out, errOut bytes.Buffer
	if code := Run([]string{"req", "workspace", "--root", fix.root, "--dev-branch", "dev", "--release-upstream", "release"}, nil, &out, &errOut); code == 0 {
		t.Fatal("missing original REQ should remain a real error")
	}
}
