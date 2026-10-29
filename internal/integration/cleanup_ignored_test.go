package integration

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/entroforge/go-system-builder/internal/workpacket"
)

func TestCleanupIgnoredInventoryPreservesVerifiedWorkAndResumes(t *testing.T) {
	for _, variant := range []string{"unknown", "changed", "symlink", "known"} {
		t.Run(variant, func(t *testing.T) {
			root, worker, in, cfg, git := newRecoveryGitRepo(t)
			checks := 0
			cfg.RequiredChecks = []string{"check"}
			cfg.CheckRunner = func(context.Context, string, string) error { checks++; return nil }
			verified, err := Integrate(context.Background(), IntegrateRequest{Inspection: in}, cfg)
			if err != nil {
				t.Fatal(err)
			}
			privatePath := filepath.Join(worker, "original-professional.json")
			privateBytes := []byte("accepted professional original")
			if err := os.WriteFile(privatePath, privateBytes, 0600); err != nil {
				t.Fatal(err)
			}
			cfg.WorkerPrivateEvidence = map[string]string{"original-professional.json": workpacket.Digest(privateBytes)}
			exclude := git(worker, "rev-parse", "--git-path", "info/exclude")
			f, err := os.OpenFile(exclude, os.O_APPEND|os.O_WRONLY, 0600)
			if err != nil {
				t.Fatal(err)
			}
			_, err = f.WriteString("\nprivate.bin\n")
			f.Close()
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(worker, "private.bin")
			original := []byte("original system bytes")
			if err := os.WriteFile(path, original, 0600); err != nil {
				t.Fatal(err)
			}
			if variant != "unknown" {
				cfg.WorkerIgnoredEvidence = map[string]string{"private.bin": workpacket.Digest(original)}
			}
			outside := filepath.Join(t.TempDir(), "outside")
			if err := os.WriteFile(outside, []byte("owner data"), 0600); err != nil {
				t.Fatal(err)
			}
			switch variant {
			case "changed":
				if err := os.WriteFile(path, []byte("new owner bytes"), 0600); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(outside, path); err != nil {
					t.Fatal(err)
				}
			}
			got, err := Integrate(context.Background(), IntegrateRequest{Inspection: in, Acknowledge: true, Cleanup: true}, cfg)
			if variant == "known" {
				if err != nil || got.Checkpoint.State != StateComplete {
					t.Fatalf("known: %+v %v", got, err)
				}
			} else {
				if !errors.Is(err, ErrCleanupPending) || got.Checkpoint.State != StateCleanupPending || !strings.Contains(err.Error(), "ignored Worker file") {
					t.Fatalf("pending: %+v %v", got, err)
				}
				if _, err := os.Lstat(path); err != nil {
					t.Fatal("source lost", err)
				}
				if raw, err := os.ReadFile(privatePath); err != nil || string(raw) != string(privateBytes) {
					t.Fatalf("original professional file removed before ignored guard: %s %v", raw, err)
				}
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				got, err = Integrate(context.Background(), IntegrateRequest{Inspection: in, Acknowledge: true, Cleanup: true}, cfg)
				if err != nil || got.Checkpoint.State != StateComplete {
					t.Fatalf("resume: %+v %v", got, err)
				}
			}
			if checks != 1 || got.Checkpoint.VerifiedAt != verified.Checkpoint.VerifiedAt {
				t.Fatalf("verification changed: checks=%d", checks)
			}
			if raw, err := os.ReadFile(outside); err != nil || string(raw) != "owner data" {
				t.Fatalf("outside changed: %s %v", raw, err)
			}
			if git(root, "rev-parse", "feature") != in.SourceHead {
				t.Fatal("source branch removed")
			}
		})
	}
}

func TestCleanupIgnoredInventoryBudgetAndCancellation(t *testing.T) {
	root, worker, _, _, git := newRecoveryGitRepo(t)
	_ = root
	exclude := git(worker, "rev-parse", "--git-path", "info/exclude")
	f, err := os.OpenFile(exclude, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.WriteString("\nignored-large/\n")
	f.Close()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(worker, "ignored-large"), 0700); err != nil {
		t.Fatal(err)
	}
	// Long names exercise the production command-output cap before bytes are
	// interpreted as proof. The error must preserve every file.
	suffix := strings.Repeat("x", 220)
	for i := 0; i < 5000; i++ {
		path := filepath.Join(worker, "ignored-large", fmt.Sprintf("%04d-%s", i, suffix))
		if err := os.WriteFile(path, nil, 0600); err != nil {
			t.Fatal(err)
		}
	}
	started := time.Now()
	err = guardIgnoredCleanup(context.Background(), worker, nil)
	t.Logf("5000 ignored files: bounded inventory wall=%s", time.Since(started))
	if err == nil || !strings.Contains(err.Error(), "limit") {
		t.Fatalf("expected inventory limit: %v", err)
	}
	entries, err := os.ReadDir(filepath.Join(worker, "ignored-large"))
	if err != nil || len(entries) != 5000 {
		t.Fatalf("inventory mutated source: %d %v", len(entries), err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := guardIgnoredCleanup(ctx, worker, nil); err == nil {
		t.Fatal("cancelled inventory succeeded")
	}
}

func TestCleanupUnknownIgnoredBlocksOtherwiseCleanShortcut(t *testing.T) {
	_, worker, in, cfg, git := newRecoveryGitRepo(t)
	verified, err := Integrate(context.Background(), IntegrateRequest{Inspection: in}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	exclude := git(worker, "rev-parse", "--git-path", "info/exclude")
	body, err := os.ReadFile(exclude)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(exclude, append(body, []byte("\nprivate.bin\n")...), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(worker, "private.bin"), []byte("owner bytes"), 0600); err != nil {
		t.Fatal(err)
	}
	if status := git(worker, "status", "--porcelain"); status != "" {
		t.Fatalf("fixture should be Git-clean: %s", status)
	}
	got, err := Integrate(context.Background(), IntegrateRequest{Inspection: in, Acknowledge: true, Cleanup: true}, cfg)
	if !errors.Is(err, ErrCleanupPending) || got.Checkpoint.VerifiedAt != verified.Checkpoint.VerifiedAt {
		t.Fatalf("clean shortcut lost protection: %+v %v", got, err)
	}
	if raw, err := os.ReadFile(filepath.Join(worker, "private.bin")); err != nil || string(raw) != "owner bytes" {
		t.Fatalf("owner bytes lost: %q %v", raw, err)
	}
}

func TestCleanupIgnoredAcceptsExactInstalledExecutableBytes(t *testing.T) {
	_, worker, _, _, git := newRecoveryGitRepo(t)
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	binary, err := os.ReadFile(executable)
	if err != nil {
		t.Fatal(err)
	}
	exclude := git(worker, "rev-parse", "--git-path", "info/exclude")
	body, err := os.ReadFile(exclude)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(exclude, append(body, []byte("\ninstalled-executable\n")...), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(worker, "installed-executable"), binary, 0700); err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	if err := guardIgnoredCleanup(context.Background(), worker, map[string]string{"installed-executable": workpacket.Digest(binary)}); err != nil {
		t.Fatal(err)
	}
	t.Logf("exact executable bytes=%d proof wall=%s", len(binary), time.Since(started))
}
