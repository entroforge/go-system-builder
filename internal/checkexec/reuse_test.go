package checkexec

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func acceptedFixture(t *testing.T) (string, ReuseRequest, ToolReceipt) {
	t.Helper()
	root := t.TempDir()
	checkout := t.TempDir()
	p := testProfile()
	p.ReusePolicy = "same_input_same_responsibility"
	observation, err := ObserveProfile(context.Background(), checkout, p)
	if err != nil {
		t.Fatal(err)
	}
	permissions := unitPermissions()
	observation.Permissions = &permissions
	observation.PermissionDigest = permissions.Digest()
	binding := Binding{RuntimeID: "runtime", Generation: 1, AssignmentID: "assignment", Owner: "builder", ExecutionGeneration: 1, Responsibility: "owned_check"}
	log := filepath.Join(root, "output.log")
	if err := os.WriteFile(log, []byte("actual test fixture output\n"), 0600); err != nil {
		t.Fatal(err)
	}
	r := map[string]any{"version": 2, "command": p.Command, "status": "pass", "output": log, "profile_observation": observation, "check_binding": binding}
	data, err := SealReceipt(r, log)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "receipt.json")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	var ref ToolReceipt
	ctx := WithReceiptCapture(WithBinding(WithProfile(context.Background(), p), binding), func(captured ToolReceipt) { ref = captured })
	if err := CaptureReceipt(ctx, path, checkout, data); err != nil {
		t.Fatal(err)
	}
	return root, ReuseRequest{Profile: p, Observation: observation, Binding: binding, Checkout: checkout}, ref
}

func TestReuseExactFactsAreShadowAndNeverSkip(t *testing.T) {
	root, current, ref := acceptedFixture(t)
	decision := DecideReuse(root, current, []ToolReceipt{ref})
	if decision.Action != "run" || !decision.WouldReuse || decision.ReceiptRef != ref.Path {
		t.Fatal(decision)
	}
	if decision := DecideReuse(root, current, nil); decision.WouldReuse || decision.Reason != "no_accepted_receipt" {
		t.Fatal("file existence was accepted", decision)
	}
}

func TestReuseBoundariesAndEveryObservedDigest(t *testing.T) {
	for _, change := range []string{"owner", "runtime", "generation", "assignment", "execution", "mandatory", "independent", "fresh", "hypothesis", "permission", "never", "environment_policy", "side_effects", "closure", "profile", "subject", "runner", "environment", "dependency", "fixture", "checkout", "incomplete_observation"} {
		t.Run(change, func(t *testing.T) {
			root, current, ref := acceptedFixture(t)
			switch change {
			case "owner":
				current.Binding.Owner = "another"
			case "runtime":
				current.Binding.RuntimeID = "another"
			case "generation":
				current.Binding.Generation++
			case "assignment":
				current.Binding.AssignmentID = "another"
			case "execution":
				current.Binding.ExecutionGeneration++
			case "mandatory":
				current.Binding.Responsibility = "main_required_integration"
			case "independent":
				current.Binding.Responsibility = "independent_review"
			case "fresh":
				current.Fresh = true
			case "hypothesis":
				current.NewHypothesis = true
			case "permission":
				current.Observation.Permissions.Available = false
				current.Observation.PermissionDigest = current.Observation.Permissions.Digest()
			case "never":
				current.Profile.ReusePolicy = "never"
			case "environment_policy":
				current.Profile.EnvironmentPolicy = "inherited"
			case "side_effects":
				current.Profile.SideEffects = "unknown"
			case "closure":
				current.Profile.InputClosure = "unknown"
			case "profile":
				current.Profile.Command = "false"
			case "subject":
				current.Observation.SubjectDigest = Hash([]byte(change))
			case "runner":
				current.Observation.RunnerDigest = Hash([]byte(change))
			case "environment":
				current.Observation.EnvironmentDigest = Hash([]byte(change))
			case "dependency":
				current.Observation.DependencyDigest = Hash([]byte(change))
			case "fixture":
				current.Observation.FixtureDigest = Hash([]byte(change))
			case "checkout":
				current.Checkout = t.TempDir()
			case "incomplete_observation":
				current.Observation.SubjectDigest = ""
			}
			if d := DecideReuse(root, current, []ToolReceipt{ref}); d.Action != "run" || d.WouldReuse {
				t.Fatal("changed authority/facts were reused", d)
			}
		})
	}
}

func TestCapturedReceiptRejectsResealLogDriftTruncationAndLegacy(t *testing.T) {
	for _, kind := range []string{"reseal", "log", "truncated", "legacy", "missing", "escape"} {
		t.Run(kind, func(t *testing.T) {
			root, current, ref := acceptedFixture(t)
			data, _ := os.ReadFile(ref.Path)
			var raw map[string]any
			json.Unmarshal(data, &raw)
			switch kind {
			case "reseal":
				raw["status"] = "fail"
				delete(raw, "receipt_integrity")
				data, _ = SealReceipt(raw, raw["output"].(string))
				os.WriteFile(ref.Path, data, 0600)
			case "log":
				os.WriteFile(raw["output"].(string), []byte("forged output"), 0600)
			case "truncated":
				os.WriteFile(ref.Path, []byte("{"), 0600)
			case "legacy":
				raw["version"] = 1
				data, _ = json.Marshal(raw)
				os.WriteFile(ref.Path, data, 0600)
			case "missing":
				os.Remove(ref.Path)
			case "escape":
				outside := filepath.Join(t.TempDir(), "receipt.json")
				os.WriteFile(outside, data, 0600)
				os.Remove(ref.Path)
				os.Symlink(outside, ref.Path)
			}
			if _, err := VerifyToolReceipt(root, ref); err == nil {
				t.Fatal("damaged source was accepted", kind)
			}
			if d := DecideReuse(root, current, []ToolReceipt{ref}); d.WouldReuse {
				t.Fatal("damaged source was reused", kind, d)
			}
		})
	}
}

func TestCaptureUsesOwnBytesAndFailedChecksCannotReuse(t *testing.T) {
	root, current, ref := acceptedFixture(t)
	data, _ := os.ReadFile(ref.Path)
	var raw map[string]any
	json.Unmarshal(data, &raw)
	raw["status"] = "fail"
	delete(raw, "receipt_integrity")
	data, _ = SealReceipt(raw, raw["output"].(string))
	os.WriteFile(ref.Path, data, 0600)
	ctx := WithReceiptCapture(WithBinding(WithProfile(context.Background(), current.Profile), current.Binding), func(r ToolReceipt) { ref = r })
	if err := CaptureReceipt(ctx, ref.Path, current.Checkout, data); err != nil {
		t.Fatal(err)
	}
	if d := DecideReuse(root, current, []ToolReceipt{ref}); d.WouldReuse || d.Reason != "previous_check_failed" {
		t.Fatal(d)
	}
	current.Binding.Owner = "another"
	ctx = WithReceiptCapture(WithBinding(WithProfile(context.Background(), current.Profile), current.Binding), func(ToolReceipt) { t.Fatal("captured forged binding") })
	if err := CaptureReceipt(ctx, ref.Path, current.Checkout, data); err == nil {
		t.Fatal("capture trusted payload identity")
	}
}

func TestExplicitEvidenceRelocationPreservesBytesButCannotReuseAnotherCheckout(t *testing.T) {
	root, current, ref := acceptedFixture(t)
	before, err := os.ReadFile(ref.Path)
	if err != nil {
		t.Fatal(err)
	}
	moved := filepath.Join(t.TempDir(), "evidence")
	if err := os.Rename(root, moved); err != nil {
		t.Fatal(err)
	}
	ref.Path = filepath.Join(moved, "receipt.json")
	verified, err := VerifyToolReceipt(moved, ref)
	if err != nil || !verified.Passed {
		t.Fatal("original bytes lost after coordinate-only move", err)
	}
	after, _ := os.ReadFile(ref.Path)
	if string(before) != string(after) {
		t.Fatal("relocation refreshed receipt")
	}
	oldCheckout := current.Checkout
	current.Checkout = filepath.Join(t.TempDir(), "worker")
	if err := os.Rename(oldCheckout, current.Checkout); err != nil {
		t.Fatal(err)
	}
	if d := DecideReuse(moved, current, []ToolReceipt{ref}); d.WouldReuse || d.Reason != "different_checkout" {
		t.Fatal("relocation authorized cross-checkout reuse", d)
	}
}

func unitPermissions() PermissionObservation {
	d := Hash([]byte("explicit unit fixture; not real Worker isolation proof"))
	return PermissionObservation{Version: "1.0.0", Runner: "worker_bubblewrap_seccomp_v1", Platform: "linux/amd64", Kernel: "unit-fixture", UID: 1000, GID: 1000, Groups: []int{1000}, BinarySHA256: d, ShellSHA256: d, LibrariesSHA256: d, FilterSHA256: d, PolicySHA256: d, AuthoritySHA256: d, MainRoot: "/main", CommonDir: "/main/.git", WorkerRoot: "/worker", CWD: "/worker", Available: true}
}

func TestExplicitEnabledReuseRequiresItsOwnAcceptedProfile(t *testing.T) {
	root, current, ref := acceptedFixture(t)
	current.Profile.Location = "worker"
	current.Profile.ReuseMode = "enabled"
	current.Observation.ProfileDigest = current.Profile.Digest()
	if d := DecideReuse(root, current, []ToolReceipt{ref}); d.Action == "reuse" {
		t.Fatal("shadow receipt was upgraded to enabled")
	}
	data, err := os.ReadFile(ref.Path)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	json.Unmarshal(data, &raw)
	raw["profile_observation"] = current.Observation
	data, err = SealReceipt(raw, raw["output"].(string))
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(ref.Path, data, 0600)
	ctx := WithReceiptCapture(WithBinding(WithProfile(context.Background(), current.Profile), current.Binding), func(r ToolReceipt) { ref = r })
	if err := CaptureReceipt(ctx, ref.Path, current.Checkout, data); err != nil {
		t.Fatal(err)
	}
	d := DecideReuse(root, current, []ToolReceipt{ref})
	if d.Action != "reuse" || d.ReceiptRef != ref.Path {
		t.Fatal("explicit own accepted profile did not reuse", d)
	}
	current.Fresh = true
	if d := DecideReuse(root, current, []ToolReceipt{ref}); d.Action != "run" {
		t.Fatal("fresh check reused", d)
	}
}
