package repair

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	runtimepkg "github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

func TestCandidateCapturedLogsDoNotHideProductOrUnreferencedChanges(t *testing.T) {
	for _, mode := range []string{"captured", "uncaptured-neighbor", "captured-source", "tracked-log", "assigned-log", "baseline-log", "changed-worker", "changed-copy"} {
		t.Run(mode, func(t *testing.T) {
			root, worker := t.TempDir(), t.TempDir()
			boundaryGit(t, worker, "init")
			boundaryGit(t, worker, "config", "user.name", "fixture")
			boundaryGit(t, worker, "config", "user.email", "fixture@example.com")
			path := "logs/check.txt"
			if mode == "captured-source" {
				path = "logs/product.go"
			}
			data := []byte("original actual check output\n")
			boundaryWrite(t, worker, path, string(data))
			boundaryWrite(t, root, "capture", string(data))
			if mode == "tracked-log" {
				boundaryGit(t, worker, "add", path)
				boundaryGit(t, worker, "commit", "-qm", "tracked fixture")
				boundaryWrite(t, worker, path, "changed tracked fixture")
			}
			e := workspace.Execution{Path: worker, Purpose: workspace.NativeDeliveryPurpose, WritePaths: []string{"internal/**"}}
			if mode == "assigned-log" {
				e.WritePaths = append(e.WritePaths, "logs/**")
			}
			session := RepairSession{}
			if mode == "baseline-log" {
				session.BaselineArtifacts = []ArtifactRef{{Path: path, SHA256: workpacket.Digest(data)}}
			}
			p := RepairResultRequest{issued: &issuedRepairResultCapability{provenance: runtimepkg.RepairResultCandidateProvenance{LocalEvidence: []runtimepkg.RepairResultLocalEvidence{{SourcePath: path, Path: "capture", SHA256: workpacket.Digest(data)}}}}}
			if mode == "uncaptured-neighbor" {
				boundaryWrite(t, worker, "logs/other.txt", "not captured")
			}
			if mode == "changed-worker" {
				boundaryWrite(t, worker, path, "later changed output")
			}
			if mode == "changed-copy" {
				boundaryWrite(t, root, "capture", "different Main bytes")
			}
			status, err := candidateProductStatus(t.Context(), root, e, session, p)
			if mode == "captured" {
				if err != nil || status != "" {
					t.Fatalf("captured output blocked: %q %v", status, err)
				}
			} else if err == nil && status == "" {
				t.Fatal("unsafe product/log change escaped candidate check")
			}
			if _, err := os.Stat(filepath.Join(worker, path)); err != nil {
				t.Fatal("consumer removed original file")
			}
			if mode == "uncaptured-neighbor" && !strings.Contains(status, "logs/other.txt") {
				t.Fatalf("neighbor lost: %s", status)
			}
		})
	}
}
