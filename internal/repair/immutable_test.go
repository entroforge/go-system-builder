package repair

import (
	"bytes"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestImmutablePublicationHasOneCompleteWinner(t *testing.T) {
	root := t.TempDir()
	name := artifactRoot + "/results/shared.json"
	var group sync.WaitGroup
	results := make(chan []byte, 2)
	for _, b := range []byte{'a', 'b'} {
		group.Go(func() {
			data := bytes.Repeat([]byte{b}, 1024*1024)
			if _, err := publishImmutableBytes(root, name, data); err == nil {
				results <- data
			}
		})
	}
	group.Wait()
	close(results)
	winners := 0
	for data := range results {
		winners++
		actual, err := os.ReadFile(filepath.Join(root, name))
		if err != nil || !bytes.Equal(actual, data) {
			t.Fatalf("published partial or overwritten bytes: %v", err)
		}
	}
	if winners != 1 {
		t.Fatalf("got %d successful publishers", winners)
	}
	staging, _ := filepath.Glob(filepath.Join(root, ".claude/operations/staging/*"))
	if len(staging) != 0 {
		t.Fatalf("completed authoring retained private staging: %v", staging)
	}
}

func TestImmutablePublicationRejectsSymlinkAndTraversal(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".claude/review"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, artifactRoot)); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{artifactRoot + "/a.json", artifactRoot + "/../../../a.json", "/tmp/a.json"} {
		if _, err := publishImmutableBytes(root, name, []byte("invalid")); err == nil {
			t.Fatalf("unsafe path accepted: %s", name)
		}
	}
	files, err := os.ReadDir(outside)
	if err != nil || len(files) != 0 {
		t.Fatalf("wrote outside repository: %v, %v", files, err)
	}
}
