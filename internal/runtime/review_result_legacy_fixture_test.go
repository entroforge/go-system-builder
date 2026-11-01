package runtime

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
)

// Legacy fixture helpers build historical data; production does not compute
// or verify the legacy digest.
func sha256Bytes(data []byte) string { sum := sha256.Sum256(data); return fmt.Sprintf("%x", sum) }
func reviewPacketProfessionalDigest(data json.RawMessage) (string, error) {
	var obj map[string]any
	if err := json.Unmarshal(data, &obj); err != nil {
		return "", err
	}
	answer := map[string]any{}
	for _, key := range []string{"claim_results", "blocked_claims", "checks", "findings", "deviations", "site_lost", "verdict", "verification_artifact_digest"} {
		if value, ok := obj[key]; ok {
			answer[key] = value
		}
	}
	raw, err := json.Marshal(answer)
	if err != nil {
		return "", err
	}
	return sha256Bytes(raw), nil
}
