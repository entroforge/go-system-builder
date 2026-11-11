package qualitygate

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestGateOutputDoesNotRegisterFingerprint(t *testing.T) {
	encoded, err := json.Marshal(Evaluation{Status: StatusSatisfied})
	if err != nil || strings.Contains(string(encoded), `"fingerprint"`) {
		t.Fatalf("new gate output registers fingerprint: %s (%v)", encoded, err)
	}
}
