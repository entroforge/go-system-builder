package review

import (
	"crypto/sha256"
	"fmt"
)

// sha256Of supplies historical fixture bytes for unrelated recovery tests.
func sha256Of(data []byte) string { sum := sha256.Sum256(data); return fmt.Sprintf("%x", sum[:]) }
