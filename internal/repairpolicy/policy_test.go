package repairpolicy

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestPolicyRequiresPinnedHumanApprovedScope(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "policy.json")
	data := []byte(`{"version":"1","approved_by":"owner","reviewer":"driver","allowed_paths":["server"],"forbidden_paths":[],"max_contracts":4}`)
	os.WriteFile(p, data, 0600)
	sha := fmt.Sprintf("%x", sha256.Sum256(data))
	if _, err := Read(root, "policy.json", sha, "owner"); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ path, sha, owner string }{{"../policy.json", sha, "owner"}, {"policy.json", sha, "other"}, {"policy.json", "bad", "owner"}} {
		if _, err := Read(root, tc.path, tc.sha, tc.owner); err == nil {
			t.Fatalf("accepted invalid policy %v", tc)
		}
	}
	os.WriteFile(p, append(data, ' '), 0600)
	if _, err := Read(root, "policy.json", sha, "owner"); err == nil {
		t.Fatal("changed authority accepted")
	}
}

func TestPolicyRejectsExpiredAndAmbiguousAuthority(t *testing.T) {
	for _, extra := range []string{`,"expires_at":"2000-01-01T00:00:00Z"`, `,"expires_at":"invalid"`, `,"unknown":true`} {
		root := t.TempDir()
		data := []byte(`{"version":"1","approved_by":"owner","reviewer":"driver","allowed_paths":["server"],"forbidden_paths":[],"max_contracts":4` + extra + `}`)
		if err := os.WriteFile(filepath.Join(root, "policy.json"), data, 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := Read(root, "policy.json", fmt.Sprintf("%x", sha256.Sum256(data)), "owner"); err == nil {
			t.Fatalf("accepted %s", extra)
		}
	}
}
