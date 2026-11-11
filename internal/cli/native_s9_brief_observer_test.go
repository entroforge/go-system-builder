package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNativeS9BriefRestoresFromOriginalPacket(t *testing.T) {
	root, _, packet, _, _ := nativeS9WorkerFixture(t)
	brief := filepath.Join(root, nativeS9BriefPath(packet))
	if err := os.Remove(brief); err != nil {
		t.Fatal(err)
	}
	before := repairDispatchRuntimeBytes(t, root)
	guidance, err := restoreNativeS9Brief(context.Background(), root, packet)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(guidance, brief) {
		t.Fatalf("guidance omits original brief path: %s", guidance)
	}
	body, err := os.ReadFile(brief)
	if err != nil {
		t.Fatal(err)
	}
	plan, result, err := nativeS9AuthorPaths(root, packet)
	if err != nil || !strings.Contains(string(body), plan) || !strings.Contains(string(body), result) {
		t.Fatalf("brief lacks original answer paths: %v", err)
	}
	repairDispatchAssertUnchanged(t, root, before)
}
