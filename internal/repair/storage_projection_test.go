package repair

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/packetbody"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

func repairStoragePacketBytes(t *testing.T) []byte {
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

func TestRepairBaselineExcludesOnlyExactIndexAndRealPacketStorage(t *testing.T) {
	root := t.TempDir()
	boundaryGit(t, root, "init")
	data := repairStoragePacketBytes(t)
	body := packetbody.Directory + packetbody.Digest(data) + ".json"
	boundaryWrite(t, root, body, string(data))
	boundaryWrite(t, root, ".claude/hook-decisions.jsonl.index", "rebuildable cache\n")
	before, _, err := captureRepositoryBaseline(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(before) != 0 {
		t.Fatalf("framework storage entered product baseline: %+v", before)
	}
	neighbors := []string{".claude/packet-bodies/product.go", ".claude/hook-decisions.jsonl.index.go", ".claude/packet-bodies/" + packetbody.Digest(data) + ".json.go"}
	for _, path := range neighbors {
		boundaryWrite(t, root, path, "package product\n")
	}
	changes, err := ComputeSessionChangeset(root, RepairSession{BaselineArtifacts: before})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, change := range changes {
		got[change.Path] = true
	}
	if len(got) != len(neighbors) {
		t.Fatalf("unexpected exact product changeset: %+v", changes)
	}
	for _, path := range neighbors {
		if !got[path] || isControlPlanePath(path, false, root) {
			t.Fatalf("neighbor became storage control: %s", path)
		}
	}
	if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(body)), []byte(`{"product":"forged"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if isControlPlanePath(body, false, root) {
		t.Fatal("filename waived changed packet bytes")
	}
	fake := []byte(`{"product":"ordinary JSON"}`)
	fakePath := packetbody.Directory + packetbody.Digest(fake) + ".json"
	boundaryWrite(t, root, fakePath, string(fake))
	if isControlPlanePath(fakePath, false, root) {
		t.Fatal("matching filename hash waived non-packet JSON")
	}
}
