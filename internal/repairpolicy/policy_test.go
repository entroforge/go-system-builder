package repairpolicy

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPolicyUsesHumanApprovedScopeWithoutDigestGate(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "policy.json")
	data := []byte(`{"version":"1","approved_by":"owner","reviewer":"driver","allowed_paths":["server"],"forbidden_paths":[],"max_contracts":4}`)
	if err := os.WriteFile(p, data, 0600); err != nil {
		t.Fatal(err)
	}
	policy, err := Read(root, "policy.json", "owner")
	if err != nil || policy.ApprovedBy != "owner" || policy.Reviewer != "driver" || len(policy.AllowedPaths) != 1 || policy.AllowedPaths[0] != "server" {
		t.Fatalf("actual approved policy lost: %+v, %v", policy, err)
	}
	for _, tc := range []struct{ path, owner string }{{"../policy.json", "owner"}, {"policy.json", "other"}} {
		if _, err := Read(root, tc.path, tc.owner); err == nil {
			t.Fatalf("accepted invalid policy %v", tc)
		}
	}
	if err := os.WriteFile(p, append(data, ' '), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Read(root, "policy.json", "owner"); err != nil {
		t.Fatalf("format-only change must not require a matching digest: %v", err)
	}
	if err := os.WriteFile(p, []byte(`{"version":"1","approved_by":"other","reviewer":"driver","allowed_paths":["server"],"forbidden_paths":[],"max_contracts":4}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Read(root, "policy.json", "owner"); err == nil {
		t.Fatal("policy naming another actual approver was accepted")
	}
	if err := os.Symlink(p, filepath.Join(root, "link.json")); err != nil {
		t.Fatal(err)
	}
	if _, err := Read(root, "link.json", "owner"); err == nil {
		t.Fatal("symlink policy path was accepted")
	}
}

func TestPolicyRejectsExpiredAndAmbiguousAuthority(t *testing.T) {
	for _, extra := range []string{`,"expires_at":"2000-01-01T00:00:00Z"`, `,"expires_at":"invalid"`, `,"unknown":true`} {
		root := t.TempDir()
		data := []byte(`{"version":"1","approved_by":"owner","reviewer":"driver","allowed_paths":["server"],"forbidden_paths":[],"max_contracts":4` + extra + `}`)
		if err := os.WriteFile(filepath.Join(root, "policy.json"), data, 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := Read(root, "policy.json", "owner"); err == nil {
			t.Fatalf("accepted %s", extra)
		}
	}
}
