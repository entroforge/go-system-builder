package runtime_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/fileview"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

func TestS5MethodsComeFromManifestAndPreserveIssuedPackets(t *testing.T) {
	for _, oldIssued := range []bool{false, true} {
		t.Run(map[bool]string{false: "new methods", true: "original issued packet"}[oldIssued], func(t *testing.T) {
			root, statePath, journal, base := packetFixture(t)
			data, err := os.ReadFile(filepath.Join(root, "manifest.json"))
			if err != nil {
				t.Fatal(err)
			}
			var manifest map[string]any
			if err := json.Unmarshal(data, &manifest); err != nil {
				t.Fatal(err)
			}
			manifest["assignments"].([]any)[0].(map[string]any)["skill_refs"] = []string{"document-verification", "api-contracts"}
			data, _ = json.Marshal(manifest)
			if err := os.WriteFile(filepath.Join(root, "manifest.json"), data, 0600); err != nil {
				t.Fatal(err)
			}
			base.Binding.AssignmentSHA = workpacket.Digest(data)
			base, err = workpacket.BuildS5(base.Binding, base.Inputs)
			if err != nil {
				t.Fatal(err)
			}
			for _, name := range []string{"document-verification", "api-contracts"} {
				path := filepath.Join(root, ".claude", "skills", name, "SKILL.md")
				if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte("# Original method\n"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if oldIssued {
				if _, err := runtime.RecordWorkPacket(root, statePath, journal, base, fileview.Disk{Root: root}, testCandidateValidator()); err != nil {
					t.Fatal(err)
				}
			}
			snapshot, err := runtime.NewStore(statePath, journal).Snapshot()
			if err != nil {
				t.Fatal(err)
			}
			packets, err := runtime.S5Packets(root, snapshot.State, base.Binding.Owner)
			if err != nil || len(packets) != 1 {
				t.Fatal("issue methods", err)
			}
			if oldIssued {
				if !reflect.DeepEqual(packets[0], base) {
					t.Fatal("old immutable packet rewritten for new guidance")
				}
				return
			}
			if len(packets[0].Methods) != 2 || packets[0].Methods[0].Path != ".claude/skills/api-contracts/SKILL.md" {
				t.Fatalf("manifest methods missing: %#v", packets[0].Methods)
			}
			if _, err := runtime.RecordWorkPacket(root, statePath, journal, packets[0], fileview.Disk{Root: root}, testCandidateValidator()); err != nil {
				t.Fatal(err)
			}
			foreign, err := workpacket.BindMethods(base, []workpacket.ReadItem{{Path: ".claude/skills/testing-strategy/SKILL.md", Purpose: packets[0].Methods[0].Purpose}})
			if err != nil {
				t.Fatal(err)
			}
			if err := runtime.ValidateWorkPacket(root, snapshot.State, foreign, fileview.Disk{Root: root}); err == nil || !strings.Contains(err.Error(), "methods differ") {
				t.Fatalf("unregistered method substituted: %v", err)
			}
		})
	}
}
