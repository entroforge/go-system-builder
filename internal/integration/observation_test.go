package integration

import (
	"context"
	"encoding/json"
	"github.com/entroforge/go-system-builder/internal/schema"
	"os"
	"path/filepath"
	"testing"
)

func TestWorkspaceObservationSharesOriginalCanonicalLock(t *testing.T) {
	root := t.TempDir()
	validate := func(o WorkspaceObservation) {
		t.Helper()
		data, _ := json.Marshal(o)
		if err := schema.NewEmbeddedValidator().ValidateBytes("integration-observation.schema.json", data); err != nil {
			t.Fatal(err)
		}
		if !o.PointInTime || o.Authorizes {
			t.Fatal("observation became authority", o)
		}
	}
	o := ObserveWorkspace(root)
	validate(o)
	if o.LockState != "not_created" {
		t.Fatal(o)
	}
	if _, err := os.Stat(filepath.Join(root, ".claude")); !os.IsNotExist(err) {
		t.Fatal("observation created control files")
	}
	_, release, err := LockWorkspace(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	o = ObserveWorkspace(filepath.Join(root, "."))
	validate(o)
	if o.LockState != "busy" {
		t.Fatal(o)
	}
	release()
	o = ObserveWorkspace(root)
	validate(o)
	if o.LockState != "available" {
		t.Fatal(o)
	}
	if err := os.Remove(o.LockRef); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(o.LockRef, 0700); err != nil {
		t.Fatal(err)
	}
	o = ObserveWorkspace(root)
	validate(o)
	if o.LockState != "unknown" || o.Diagnostic == "" {
		t.Fatal(o)
	}
	if err := os.Remove(o.LockRef); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "target.lock")
	if err := os.WriteFile(target, []byte("target"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, o.LockRef); err != nil {
		t.Skip("host cannot create symlink observation fixture")
	}
	o = ObserveWorkspace(root)
	validate(o)
	if o.LockState != "unknown" {
		t.Fatal("symlink used as a trusted lock", o)
	}
	data, _ := os.ReadFile(target)
	if string(data) != "target" {
		t.Fatal("probe modified linked target")
	}
}
