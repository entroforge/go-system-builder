// Package repairpolicy pins a project-approved technical-repair policy at REQ
// binding. It creates no human decision and never changes existing runtimes.
package repairpolicy

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Policy struct {
	Version        string   `json:"version"`
	ApprovedBy     string   `json:"approved_by"`
	Reviewer       string   `json:"reviewer"`
	AllowedPaths   []string `json:"allowed_paths"`
	ForbiddenPaths []string `json:"forbidden_paths"`
	MaxContracts   int      `json:"max_contracts"`
	ExpiresAt      string   `json:"expires_at,omitempty"`
}

func Read(root, rel, digest, approvedBy string) (Policy, error) {
	var p Policy
	if rel == "" || len(digest) != 64 || filepath.IsAbs(rel) || filepath.ToSlash(filepath.Clean(rel)) != rel || strings.ContainsAny(rel, "\\*?[]:") || rel == ".." || strings.HasPrefix(rel, "../") {
		return p, fmt.Errorf("repair policy requires a relative path and explicit SHA256")
	}
	cur := root
	for _, part := range strings.Split(rel, "/") {
		cur = filepath.Join(cur, part)
		i, e := os.Lstat(cur)
		if e != nil {
			return p, e
		}
		if i.Mode()&os.ModeSymlink != 0 {
			return p, fmt.Errorf("repair policy symlink is not allowed")
		}
	}
	data, err := os.ReadFile(cur)
	if err != nil {
		return p, err
	}
	h := sha256.Sum256(data)
	if hex.EncodeToString(h[:]) != digest {
		return p, fmt.Errorf("repair policy fingerprint mismatch")
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err = d.Decode(&p); err != nil {
		return p, err
	}
	if d.Decode(new(any)) != io.EOF {
		return p, fmt.Errorf("repair policy must be one JSON object")
	}
	if p.Version != "1" || p.ApprovedBy == "" || p.ApprovedBy != approvedBy || p.Reviewer == "" || p.Reviewer == p.ApprovedBy || len(p.AllowedPaths) == 0 || p.MaxContracts < 1 {
		return p, fmt.Errorf("repair policy requires version 1, matching human approver, independent reviewer, bounded paths and positive budget")
	}
	for _, v := range append(append([]string{}, p.AllowedPaths...), p.ForbiddenPaths...) {
		if v == "" || v == "." || v == ".." || filepath.IsAbs(v) || strings.HasPrefix(v, "../") || strings.ContainsAny(v, "\\*?[]:") || filepath.ToSlash(filepath.Clean(v)) != v {
			return p, fmt.Errorf("repair policy paths must be literal repository-relative paths")
		}
	}
	if p.ExpiresAt != "" {
		expiry, err := time.Parse(time.RFC3339, p.ExpiresAt)
		if err != nil || !time.Now().Before(expiry) {
			return p, fmt.Errorf("repair policy expiry invalid or expired")
		}
	}
	return p, nil
}
