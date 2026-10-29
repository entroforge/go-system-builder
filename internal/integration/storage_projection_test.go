package integration

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/packetbody"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

func projectionPacketBytes(t *testing.T) []byte {
	t.Helper()
	p, err := workpacket.BuildReviewResult(workpacket.ReviewResultBinding{
		RuntimeID: "loop-projection", BaselineGeneration: 1, ReviewPlanID: "review-plan-projection", ReviewPlanRef: ".claude/review/plan.json", ReviewPlanSHA256: strings.Repeat("a", 64), ReviewPlanRevision: 1, ReviewRound: 1,
		AssignmentID: "assignment-qa-projection", AssignmentSHA256: strings.Repeat("b", 64), OwnerAgentID: "reviewer-projection", ClaimIDs: []string{"claim-projection"}, SubjectDigest: strings.Repeat("c", 64),
	}, workpacket.ReviewResultInputs{Claims: json.RawMessage(`[{"claim_id":"claim-projection","lens":"qa","target":"product behavior","assertion":"product is correct","oracle":"matches requirements","method":"inspect original product","applicability":"required","source_refs":["REQ-projection"]}]`), FrozenSubjects: json.RawMessage(`[{"path":"product.go","sha256":"dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"}]`)})
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func writeProjectionObject(t *testing.T, root, path string, data []byte) {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(full), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, data, 0600); err != nil {
		t.Fatal(err)
	}
}

func TestWorktreeProjectionVerifiesRealPacketBodiesAndKeepsNeighborsDirty(t *testing.T) {
	root := t.TempDir()
	gitRevisionTest(t, root, "init")
	data := projectionPacketBytes(t)
	digest := packetbody.Digest(data)
	body := packetbody.Directory + digest + ".json"
	copy := ".claude/runtime-checkpoints/" + strings.Repeat("e", 64) + "/packet-bodies/" + digest + ".json"
	for _, path := range []string{body, copy} {
		writeProjectionObject(t, root, path, data)
	}
	writeProjectionObject(t, root, ".claude/hook-decisions.jsonl.index", []byte("rebuildable cache\n"))
	if clean, err := worktreeCleanWithProjection(context.Background(), root, nil); err != nil || !clean {
		t.Fatalf("actual framework storage made checkout dirty: %t %v", clean, err)
	}
	for _, neighbor := range []string{
		".claude/hook-decisions.jsonl.index.go",
		".claude/packet-bodies/product.go",
		".claude/packet-bodies/" + digest + ".json.go",
		".claude/runtime-checkpoints/" + strings.Repeat("e", 64) + "/packet-bodies/product.go",
	} {
		t.Run(neighbor, func(t *testing.T) {
			writeProjectionObject(t, root, neighbor, []byte("package product\n"))
			defer os.Remove(filepath.Join(root, filepath.FromSlash(neighbor)))
			if clean, err := worktreeCleanWithProjection(context.Background(), root, nil); err != nil || clean {
				t.Fatalf("neighbor acquired storage exemption: %t %v", clean, err)
			}
		})
	}
	writeProjectionObject(t, root, body, []byte(`{"product":"forged under original hash"}`))
	if clean, err := worktreeCleanWithProjection(context.Background(), root, nil); err == nil || clean {
		t.Fatalf("content-addressed filename waived changed bytes: %t %v", clean, err)
	}
}

func TestPacketProjectionRequiresCompleteCanonicalPacketNotOnlyHash(t *testing.T) {
	root := t.TempDir()
	data := []byte(`{"product":"ordinary JSON"}`)
	path := packetbody.Directory + packetbody.Digest(data) + ".json"
	writeProjectionObject(t, root, path, data)
	if control, err := runtimeBodyProjection(context.Background(), root, path); err == nil || control {
		t.Fatalf("arbitrary hash-named JSON became framework storage: %t %v", control, err)
	}
	for _, path := range []string{".claude/hook-decisions.jsonl.index.go", ".claude/packet-bodies/../product.go", ".claude/packet-bodies/not-a-hash.json"} {
		if runtimeProjection(path) {
			t.Fatalf("neighbor classified as Runtime projection: %s", path)
		}
	}
}
