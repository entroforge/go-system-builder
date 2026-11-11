package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestE2ECoverageOriginalCLIReportsScoreWithoutDigest(t *testing.T) {
	path := filepath.Join(t.TempDir(), "inventory.json")
	content := `{"version":"test","scenarios":[{"id":"CT-1","set":"CT","fidelity":"L2","test_refs":["real-test"]}]}`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	var out, stderr bytes.Buffer
	if code := Run([]string{"e2e-coverage", "--inventory", path}, nil, &out, &stderr); code != 0 {
		t.Fatalf("coverage: exit %d, %s", code, stderr.String())
	}
	if !strings.Contains(out.String(), "ID_Presence (CT∪AC): 1.000 (1/1)") || !strings.Contains(out.String(), "FidelityScore (CT∪AC): 0.400") || !strings.Contains(out.String(), "E2E ready gate: FAIL") {
		t.Fatalf("professional score changed: %s", out.String())
	}
	if strings.Contains(strings.ToLower(out.String()), "sha-256") || strings.Contains(strings.ToLower(out.String()), "sha256") {
		t.Fatalf("report emitted an unsolicited machine digest: %s", out.String())
	}
	out.Reset()
	stderr.Reset()
	if code := Run([]string{"e2e-coverage", "--inventory", path, "--gate"}, nil, &out, &stderr); code != 1 {
		t.Fatalf("explicit gate must still return the actual failed score, exit %d: %s", code, out.String())
	}
}
