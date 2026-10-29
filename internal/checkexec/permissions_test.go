package checkexec

import (
	"context"
	"encoding/json"
	"os"
	"testing"
)

func TestClosedStaticCheckRejectsUnobservedHostAndShellInputs(t *testing.T) {
	for _, command := range []string{"true", "false", "test -f src/product.go", "test -s 'src/file with space.txt'"} {
		if !ClosedStaticCheck(command) {
			t.Fatal("closed static command rejected", command)
		}
	}
	for _, command := range []string{"go test ./...", "./check.sh", "exec true", "command true", "test -f /etc/passwd", "test -f ../secret", "test -f .claude/loop-state.json", "test -f .git/config", "test -f .env", "test -f src/.env.local", "test -f src/*.go", "test -f $HOME/secret", "test -f \"$(pwd)/file\"", "true && false", "true; false", "test -f src/file # ignored", "test -f src/file\ntrue", "test -f .worktrees/other/file", "test -d src", "test -f ''"} {
		if ClosedStaticCheck(command) {
			t.Fatal("unclosed command admitted", command)
		}
	}
}

func removePriorPermissions(t *testing.T, current ReuseRequest, ref ToolReceipt) ToolReceipt {
	t.Helper()
	data, err := os.ReadFile(ref.Path)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	observation := raw["profile_observation"].(map[string]any)
	delete(observation, "permissions")
	delete(observation, "permission_digest")
	data, err = SealReceipt(raw, raw["output"].(string))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ref.Path, data, 0600); err != nil {
		t.Fatal(err)
	}
	ctx := WithReceiptCapture(WithBinding(WithProfile(context.Background(), current.Profile), current.Binding), func(captured ToolReceipt) { ref = captured })
	if err := CaptureReceipt(ctx, ref.Path, current.Checkout, data); err != nil {
		t.Fatal(err)
	}
	return ref
}

func TestPermissionsAreActualFactsAndNotOnlyAvailability(t *testing.T) {
	p := unitPermissions()
	if !p.Known() {
		t.Fatal("complete synthetic facts rejected")
	}
	for _, kind := range []string{"version", "runner", "platform", "kernel", "uid", "groups", "binary", "shell", "libraries", "filter", "policy", "authority", "cwd", "unavailable", "diagnostic"} {
		t.Run(kind, func(t *testing.T) {
			q := p
			switch kind {
			case "version":
				q.Version = "legacy"
			case "runner":
				q.Runner = "main_shell"
			case "platform":
				q.Platform = ""
			case "kernel":
				q.Kernel = ""
			case "uid":
				q.UID = -1
			case "groups":
				q.Groups = nil
			case "binary":
				q.BinarySHA256 = ""
			case "shell":
				q.ShellSHA256 = ""
			case "libraries":
				q.LibrariesSHA256 = ""
			case "filter":
				q.FilterSHA256 = ""
			case "policy":
				q.PolicySHA256 = ""
			case "authority":
				q.AuthoritySHA256 = ""
			case "cwd":
				q.CWD = "/different"
			case "unavailable":
				q.Available = false
			case "diagnostic":
				q.Diagnostic = "probe failed"
			}
			if q.Known() {
				t.Fatal("partial facts became known", kind)
			}
		})
	}
}

func TestReuseRequiresMatchingPermissionFactsAndKnownPriorObservation(t *testing.T) {
	for _, kind := range []string{"changed_authority", "changed_filter", "changed_library", "forged_digest", "missing_current", "missing_previous"} {
		t.Run(kind, func(t *testing.T) {
			root, current, ref := acceptedFixture(t)
			switch kind {
			case "changed_authority":
				current.Observation.Permissions.AuthoritySHA256 = Hash([]byte(kind))
				current.Observation.PermissionDigest = current.Observation.Permissions.Digest()
			case "changed_filter":
				current.Observation.Permissions.FilterSHA256 = Hash([]byte(kind))
				current.Observation.PermissionDigest = current.Observation.Permissions.Digest()
			case "changed_library":
				current.Observation.Permissions.LibrariesSHA256 = Hash([]byte(kind))
				current.Observation.PermissionDigest = current.Observation.Permissions.Digest()
			case "forged_digest":
				current.Observation.PermissionDigest = Hash([]byte(kind))
			case "missing_current":
				current.Observation.Permissions = nil
			case "missing_previous":
				ref = removePriorPermissions(t, current, ref)
			}
			if d := DecideReuse(root, current, []ToolReceipt{ref}); d.WouldReuse {
				t.Fatal("changed/unknown permission facts reused", d)
			}
		})
	}
}
