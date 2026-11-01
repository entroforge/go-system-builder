package qualitygate

import (
	"crypto/sha256"
	"encoding/hex"
)

// Legacy fixtures use historical digest-shaped values; production never computes them.
func sha256Hex(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }
