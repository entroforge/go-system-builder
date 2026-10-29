package metrics

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A separate test process can pause the actual Store mutation and publisher at
// creation, a partial write, and immediately before its atomic rename.
func TestMetricsControlledPublisher(t *testing.T) {
	root := os.Getenv("GSB_METRICS_TEST_ROOT")
	if root == "" {
		t.Skip("only used by the synchronized reception test")
	}
	input := bufio.NewReader(os.Stdin)
	wait := func(stage string, file *os.File) error {
		if _, err := fmt.Printf("STAGE %s %s\n", stage, file.Name()); err != nil {
			return err
		}
		_, err := input.ReadString('\n')
		return err
	}
	store := NewStore(root)
	err := store.mutateWith(func(s *Snapshot) { s.S7ResultSubmits["controlled"]++ }, func(path string, data []byte) error {
		var file *os.File
		return atomicWriteMetricsWith(path, data, metricsWriteHooks{
			created: func(f *os.File) error {
				file = f
				return wait("created", f)
			},
			write: func(f *os.File, data []byte) error {
				mid := len(data) / 2
				if _, err := f.Write(data[:mid]); err != nil {
					return err
				}
				if err := wait("partial", f); err != nil {
					return err
				}
				_, err := f.Write(data[mid:])
				return err
			},
			beforeRename: func() error { return wait("rename", file) },
		})
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestMetricsTemporaryNameAndFilesystemBoundary(t *testing.T) {
	good := []string{
		".claude/.loop-metrics-v1-" + strings.Repeat("a1", 16) + ".tmp",
		".claude/.loop-metrics-0.tmp",
		".claude/.loop-metrics-4294967295.tmp",
	}
	bad := []string{
		".claude/.loop-metrics-4294967296.tmp", ".claude/.loop-metrics-01.tmp",
		".claude/.loop-metrics-.tmp", ".claude/.loop-metrics-abc.tmp",
		".claude/.loop-metrics-v1-" + strings.Repeat("A", 32) + ".tmp",
		".claude/.loop-metrics-v1-" + strings.Repeat("a", 31) + ".tmp",
		".claude/sub/.loop-metrics-42.tmp", "../.claude/.loop-metrics-42.tmp",
		".claude/.loop-metrics-42.tmp/child", ".claude/.loop-metrics-42.tmp.more",
	}
	for _, path := range good {
		if !IsTemporaryPath(path) {
			t.Errorf("valid name rejected: %s", path)
		}
	}
	for _, path := range bad {
		if IsTemporaryPath(path) {
			t.Errorf("nonproducer name admitted: %s", path)
		}
	}
	root := t.TempDir()
	path := good[0]
	if ok, err := RecognizeTemporary(context.Background(), root, path); err != nil || ok {
		t.Fatalf("missing parent granted rename exception: %v %v", ok, err)
	}
	dir := filepath.Join(root, ".claude")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if ok, err := RecognizeTemporary(context.Background(), root, path); err != nil || !ok {
		t.Fatalf("missing leaf after status did not allow rename: %v %v", ok, err)
	}
	leaf := filepath.Join(root, filepath.FromSlash(path))
	if err := os.WriteFile(leaf, []byte("partial"), 0600); err != nil {
		t.Fatal(err)
	}
	if ok, err := RecognizeTemporary(context.Background(), root, path); err != nil || !ok {
		t.Fatalf("partial regular file: %v %v", ok, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := RecognizeTemporary(ctx, root, path); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation = %v", err)
	}
	if _, err := RecognizeTemporary(context.Background(), filepath.Join(root, "missing"), path); err == nil {
		t.Fatal("unreadable root error was suppressed")
	}
	if err := os.Remove(leaf); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(leaf, 0700); err != nil {
		t.Fatal(err)
	}
	if ok, err := RecognizeTemporary(context.Background(), root, path); err != nil || ok {
		t.Fatalf("directory granted exception: %v %v", ok, err)
	}
	if err := os.Remove(leaf); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "other"), leaf); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if ok, err := RecognizeTemporary(context.Background(), root, path); err != nil || ok {
		t.Fatalf("leaf symlink granted exception: %v %v", ok, err)
	}
	if err := os.Remove(leaf); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(dir); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(t.TempDir(), dir); err != nil {
		t.Skipf("parent symlink unavailable: %v", err)
	}
	if ok, err := RecognizeTemporary(context.Background(), root, path); err != nil || ok {
		t.Fatalf("symlink parent granted exception: %v %v", ok, err)
	}
}
