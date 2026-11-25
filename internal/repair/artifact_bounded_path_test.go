package repair

import (
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/workpacket"
)

// A real repair brief registered a 9,479-character issued responsibility:
// per-component chopping kept every name legal while the total path exceeded
// the kernel PATH_MAX, so no process could open the persisted artifact. The
// bounded key collapses such identities into one deterministic component;
// short ids keep their exact historical paths.
func TestLongIssuedIdentityMapsToOneBoundedDeterministicComponent(t *testing.T) {
	long := "repair-session-" + strings.Repeat("WyJsb29wLVJFUS0wMDEiXSwi", 400)
	key := workpacket.BoundedPacketPathKey(long)
	if len(key) > 120 {
		t.Fatalf("bounded key exceeds one legal component: %d", len(key))
	}
	if key == long {
		t.Fatal("long identity stored verbatim")
	}

	cases := []struct {
		name string
		path string
	}{
		{"responsibilityArtifactPath", responsibilityArtifactPath("plan-reports", long)},
		{"s9PacketEvidenceDirectory", s9PacketEvidenceDirectory(long)},
		// QuestionnaireRoot hashes its own base64 pair encoding, so only the
		// bounded shape applies here; the raw-id cases also pin the exact key.
		{"QuestionnaireRoot", QuestionnaireRoot(long, "session-1")},
	}
	for _, tc := range cases {
		if len(tc.path) > 4096 {
			t.Fatalf("%s: path exceeds the kernel limit: %d", tc.name, len(tc.path))
		}
		if strings.Count(tc.path, "/") > 6 {
			t.Fatalf("%s: chopped chain survived: %q", tc.name, tc.path)
		}
	}
	if !strings.Contains(cases[0].path, key) || !strings.Contains(cases[1].path, key) {
		t.Fatalf("deterministic key missing: %q / %q", cases[0].path, cases[1].path)
	}
	if again := QuestionnaireRoot(long, "session-1"); again != cases[2].path {
		t.Fatalf("questionnaire root not deterministic: %q vs %q", again, cases[2].path)
	}
	if again := responsibilityArtifactPath("plan-reports", long); again != cases[0].path {
		t.Fatalf("identity path not deterministic: %q vs %q", again, cases[0].path)
	}
}

func TestShortIssuedIdentityKeepsHistoricalPaths(t *testing.T) {
	if got := responsibilityArtifactPath("plan-reports", "repair-plan-s9-001"); got != ".claude/review/repair/plan-reports/repair-plan-s9-001.json" {
		t.Fatalf("short artifact path changed: %q", got)
	}
	if got := s9PacketEvidenceDirectory("packet-s9-001"); got != ".claude/evidence/s9/p/packet-s9-001" {
		t.Fatalf("short evidence directory changed: %q", got)
	}
	if got := QuestionnaireRoot("loop-REQ-001", "session-1"); !strings.HasPrefix(got, ".claude/submissions/s9/p/") || strings.Contains(got, "h0") {
		t.Fatalf("short questionnaire root changed: %q", got)
	}
}
