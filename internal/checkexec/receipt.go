package checkexec

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
)

func FileDigest(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// SealReceipt detects corruption; it is not approval or authenticated domain
// acceptance. G05 must also verify original registered tool receipt provenance.
func SealReceipt(receipt map[string]any, logPath string) ([]byte, error) {
	logDigest, err := FileDigest(logPath)
	if err != nil {
		return nil, err
	}
	receipt["log_sha256"] = logDigest
	delete(receipt, "receipt_integrity")
	data, err := json.Marshal(receipt)
	if err != nil {
		return nil, err
	}
	integrity, err := ReceiptIntegrity(data)
	if err != nil {
		return nil, err
	}
	receipt["receipt_integrity"] = integrity
	data, err = json.MarshalIndent(receipt, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

// Canonicalize nested objects as well as the top-level map. JSON struct field
// order is different from map order; preserving numbers avoids float rounding.
func ReceiptIntegrity(data []byte) (string, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var raw map[string]any
	if err := decoder.Decode(&raw); err != nil {
		return "", err
	}
	delete(raw, "receipt_integrity")
	canonical, err := json.Marshal(raw)
	if err != nil {
		return "", err
	}
	return Hash(canonical), nil
}
