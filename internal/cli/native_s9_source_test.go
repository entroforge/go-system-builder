package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/entroforge/go-system-builder/internal/repair"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/submission"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

// Real Git/Stop adapter and original consumers; professional answers are
// synthetic fixtures. This does not prove the Claude cleanup event window.
func TestNativeS9RetainedSourceRejectsLostOrChangedInputs(t *testing.T) {
	for _, change := range []string{"capture", "truncated-capture", "evidence", "git-ref", "moved-ref", "object", "typed", "generation", "flat", "product", "branch", "legacy", "cancel-during-git"} {
		t.Run(change, func(t *testing.T) {
			root, worker, p, input, _ := nativeS9WorkerFixture(t)
			// Remove only this test fixture's product contribution: retain its actual
			// original common commit, with no author seal or empty commit.
			repairLeadGit(t, worker, "reset", "--hard", repairLeadGit(t, root, "rev-parse", "HEAD"))
			input.Event = "SubagentStop"
			var diagnostic bytes.Buffer
			if _, code := nativeS9Hook(root, input, &bytes.Buffer{}, &diagnostic); code != 0 {
				t.Fatalf("Stop: %s", &diagnostic)
			}
			snap := repairDispatchSnapshot(t, root)
			registry, err := workspace.Decode(snap.State)
			if err != nil || registry == nil {
				t.Fatalf("source: %v %s", err, &diagnostic)
			}
			e := registry.Executions[p.Binding.ManifestAssignmentID]
			source, err := repair.RetainedNativeS9Source(context.Background(), root, e, p.Binding)
			if err != nil || source == nil || e.DeliveryRef == "" {
				t.Fatalf("no retained candidate: %v %s", err, &diagnostic)
			}
			ref := repair.NativeS9SourceReference(e)
			before := repairDispatchRuntimeBytes(t, root)
			if change == "cancel-during-git" {
				wrapperRoot := t.TempDir()
				marker := filepath.Join(wrapperRoot, "git-started")
				if err := os.WriteFile(filepath.Join(wrapperRoot, "git"), []byte("#!/bin/sh\nprintf started > \"$NATIVE_S9_TEST_MARKER\"\nexec /bin/sleep 30\n"), 0700); err != nil {
					t.Fatal(err)
				}
				t.Setenv("NATIVE_S9_TEST_MARKER", marker)
				t.Setenv("PATH", wrapperRoot+string(os.PathListSeparator)+os.Getenv("PATH"))
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				done := make(chan struct{})
				go func() {
					defer close(done)
					tick := time.NewTicker(time.Millisecond)
					defer tick.Stop()
					for {
						select {
						case <-ctx.Done():
							return
						case <-tick.C:
							if _, err := os.Stat(marker); err == nil {
								cancel()
								return
							}
						}
					}
				}()
				got, err := repair.RetainedNativeS9Source(ctx, root, e, p.Binding)
				<-done
				if got != nil || !errors.Is(err, context.Canceled) {
					t.Fatalf("mid-Git cancellation became a source conflict: %+v %v", got, err)
				}
				if _, err := os.Stat(marker); err != nil {
					t.Fatalf("test did not enter Git: %v", err)
				}
				repairDispatchAssertUnchanged(t, root, before)
				return
			}

			switch change {
			case "typed":
				packets, err := runtime.CurrentRepairResultPacketsContext(context.Background(), root, snap.State, p.Binding.OwnerAgentID)
				if err != nil || len(packets) != 1 {
					t.Fatalf("typed packet: %v %+v", err, packets)
				}
				typedPath := filepath.Join(root, repair.NativeS9TypedInputPath(source, packets[0]))
				body, err := os.ReadFile(typedPath)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(typedPath, append(body, '\n'), 0600); err != nil {
					t.Fatal(err)
				}
				if _, err := repair.ReadRepairResultInput(context.Background(), root, snap.State, packets[0], e); err == nil {
					t.Fatal("changed typed system wrapper was replaced")
				}
				repairDispatchAssertUnchanged(t, root, before)
				return
			case "object":
				objectPath := repairLeadGit(t, root, "rev-parse", "--git-path", "objects/"+source.Head[:2]+"/"+source.Head[2:])
				if !filepath.IsAbs(objectPath) {
					objectPath = filepath.Join(root, objectPath)
				}
				if err := os.Remove(objectPath); err != nil {
					t.Fatal(err)
				}
			case "moved-ref":
				repairLeadGit(t, root, "update-ref", source.GitRef, repairLeadGit(t, root, "rev-parse", "HEAD^"))
			case "truncated-capture":
				body, err := os.ReadFile(filepath.Join(root, ref.Path))
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(root, ref.Path), body[:len(body)/2], 0600); err != nil {
					t.Fatal(err)
				}
			case "capture":
				body, err := os.ReadFile(filepath.Join(root, ref.Path))
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(root, ref.Path), append(body, '\n'), 0600); err != nil {
					t.Fatal(err)
				}
			case "evidence":
				if len(source.Evidence) == 0 {
					t.Fatal("fixture did not capture actual file")
				}
				if err := os.Remove(filepath.Join(root, source.Evidence[0].Path)); err != nil {
					t.Fatal(err)
				}
			case "git-ref":
				repairLeadGit(t, root, "update-ref", "-d", source.GitRef)
			case "generation":
				e.Generation++
			case "flat":
				if err := os.WriteFile(filepath.Join(worker, source.ResultPath), append(source.Result, '\n'), 0600); err != nil {
					t.Fatal(err)
				}
			case "product":
				writeRepairLeadFile(t, worker, "internal/api/one.go", "package api\nconst Drift = true\n")
			case "branch":
				repairLeadGit(t, worker, "switch", "-c", "unrelated-replacement")
			case "legacy":
				e.Inputs = map[string]string{}
			}
			got, err := repair.RetainedNativeS9Source(context.Background(), root, e, p.Binding)
			if change == "legacy" {
				if err != nil || got != nil {
					t.Fatalf("legacy gained capability: %+v %v", got, err)
				}
			} else if err == nil {
				t.Fatalf("changed %s retained a recovery capability", change)
			}
			repairDispatchAssertUnchanged(t, root, before)
		})
	}
}

func TestNativeS9LegacyPinCannotAcquireRetentionOnLaterStop(t *testing.T) {
	root, worker, p, input, _ := nativeS9WorkerFixture(t)
	base := repairLeadGit(t, root, "rev-parse", "HEAD")
	repairLeadGit(t, worker, "reset", "--hard", base)
	_, pin, _ := nativeS9Paths(p)
	legacy := []byte(`{"base_commit":"` + base + `"}`)
	if err := os.WriteFile(filepath.Join(root, pin), legacy, 0600); err != nil {
		t.Fatal(err)
	}
	beforePacket, err := workpacket.RepairPlanReportPacketDigest(p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := prepareNativeS9Brief(context.Background(), root, p); err != nil {
		t.Fatal(err)
	}
	retainedPin, err := os.ReadFile(filepath.Join(root, pin))
	if err != nil || !bytes.Equal(retainedPin, legacy) {
		t.Fatalf("legacy frozen pin was upgraded: %s %v", retainedPin, err)
	}
	input.Event = "SubagentStop"
	var diagnostic bytes.Buffer
	nativeS9Hook(root, input, &bytes.Buffer{}, &diagnostic)
	snap := repairDispatchSnapshot(t, root)
	registry, err := workspace.Decode(snap.State)
	if err != nil || registry == nil {
		t.Fatalf("legacy source: %v %s", err, &diagnostic)
	}
	e := registry.Executions[p.Binding.ManifestAssignmentID]
	if e.DeliveryRef == "" || repair.NativeS9SourceReference(e).Path != "" {
		t.Fatalf("legacy consumer was replaced or upgraded: %+v %s", e, &diagnostic)
	}
	packets, err := runtime.CurrentRepairPlanReportPacketsContext(context.Background(), root, snap.State, p.Binding.OwnerAgentID)
	if err != nil {
		t.Fatal(err)
	}
	for _, current := range packets {
		if current.ID == p.ID {
			after, err := workpacket.RepairPlanReportPacketDigest(current)
			if err != nil || beforePacket != after {
				t.Fatalf("original issued packet changed: %v", err)
			}
		}
	}
}

func TestNativeS9RetainedSourceHonorsCanceledRequestBeforeIO(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	root := filepath.Join(t.TempDir(), "missing-root")
	if _, err := repair.RetainedNativeS9Source(ctx, root, workspace.Execution{}, workpacket.RepairPlanReportBinding{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("retained reader lost cancellation: %v", err)
	}
	if _, err := repair.ReadRepairResultInput(ctx, root, nil, workpacket.RepairResultPacket{}, workspace.Execution{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("typed reader lost cancellation: %v", err)
	}
	if _, _, err := submission.RepairResultRegisteredSource(root, nil, workpacket.RepairResultPacket{}, ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("selector lost cancellation: %v", err)
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatalf("canceled reader wrote a root: %v", err)
	}
}
