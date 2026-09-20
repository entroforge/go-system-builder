package cli_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"github.com/entroforge/go-system-builder/internal/cli"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestREQBindPinsApprovedRepairPolicy(t *testing.T) {
	root := newUXTestRoot(t, map[string]string{"REQ-098.md": lockedReqBody})
	data := []byte(`{"version":"1","approved_by":"ux-owner","reviewer":"driver","allowed_paths":["web","server"],"forbidden_paths":[],"max_contracts":8}`)
	os.WriteFile(filepath.Join(root, "repair-policy.json"), data, 0600)
	sha := fmt.Sprintf("%x", sha256.Sum256(data))
	var out, errout bytes.Buffer
	if code := cli.Run([]string{"req", "bind", "--root", root, "--approved-by", "ux-owner", "--repair-policy", "repair-policy.json", "--repair-policy-sha256", sha}, strings.NewReader(""), &out, &errout); code != 0 {
		t.Fatalf("bind: %s", errout.String())
	}
	raw, _ := os.ReadFile(filepath.Join(root, ".claude/loop-state.json"))
	var s map[string]any
	json.Unmarshal(raw, &s)
	pin := s["configuration"].(map[string]any)["repair"].(map[string]any)["bound_policy"].(map[string]any)
	if pin["sha256"] != sha || pin["runtime_id"] != s["runtime_id"] || pin["approved_by"] != "ux-owner" {
		t.Fatalf("pin=%v", pin)
	}
}

func TestREQBindInvalidPolicyLeavesRuntimeUnchanged(t *testing.T) {
	root := newUXTestRoot(t, map[string]string{"REQ-098.md": lockedReqBody})
	path := filepath.Join(root, ".claude/loop-state.json")
	var initOut, initErr bytes.Buffer
	if code := cli.Run([]string{"init", "--root", root}, strings.NewReader(""), &initOut, &initErr); code != 0 {
		t.Fatalf("init: %s", initErr.String())
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var out, errout bytes.Buffer
	code := cli.Run([]string{"req", "bind", "--root", root, "--approved-by", "ux-owner", "--repair-policy", "missing.json", "--repair-policy-sha256", strings.Repeat("a", 64)}, strings.NewReader(""), &out, &errout)
	if code == 0 {
		t.Fatal("missing policy accepted")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("invalid bind changed runtime")
	}
}
