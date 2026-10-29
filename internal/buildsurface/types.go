// Package buildsurface reads the original S6 contribution and product baseline
// sources for S7/S10. It owns no Writer or professional verdict.
package buildsurface

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/entroforge/go-system-builder/internal/assignmentrecord"
)

const Contract = "s7-product-baseline-v1"

type Task struct {
	ID      string `json:"id"`
	Path    string `json:"path"`
	Version string `json:"version"`
	SHA256  string `json:"sha256"`
}

type Contribution struct {
	AssignmentRecord       assignmentrecord.RecordReference `json:"assignment_record"`
	AssignmentRecordSHA256 string                           `json:"assignment_record_sha256"`
	ResultRef              string                           `json:"result_ref"`
	ResultSHA256           string                           `json:"result_sha256"`
	CheckpointRef          string                           `json:"checkpoint_ref"`
	CheckpointSHA256       string                           `json:"checkpoint_sha256"`
	MergeCommit            string                           `json:"merge_commit"`
	TestedHead             string                           `json:"tested_head"`
	MainFactsSHA256        string                           `json:"main_facts_sha256"`
}

type Path struct {
	Path            string `json:"path"`
	Status          string `json:"status"`
	SHA256          string `json:"sha256"`
	BlobOID         string `json:"blob_oid"`
	PreimageCommit  string `json:"preimage_commit,omitempty"`
	DeletedByCommit string `json:"deleted_by_commit,omitempty"`
}

type Snapshot struct {
	Contract           string         `json:"contract"`
	RuntimeID          string         `json:"runtime_id"`
	BaselineGeneration int            `json:"baseline_generation"`
	StartReceiptSHA256 string         `json:"start_receipt_sha256"`
	StartCommit        string         `json:"start_commit"`
	FinalCommit        string         `json:"final_commit"`
	ProjectRoot        string         `json:"project_root"`
	DevelopmentBranch  string         `json:"development_branch"`
	Tasks              []Task         `json:"tasks"`
	Contributions      []Contribution `json:"contributions"`
	Paths              []Path         `json:"paths"`
	UnattributedPaths  []string       `json:"unattributed_paths,omitempty"`
	Digest             string         `json:"digest"`
}

// Digest binds the typed machine projection. Read constructs sorted arrays;
// Verify compares the entire reconstructed projection, including its digest.
func Digest(snapshot Snapshot) (string, error) {
	if snapshot.Contract != Contract || snapshot.RuntimeID == "" || snapshot.BaselineGeneration < 1 {
		return "", fmt.Errorf("invalid canonical product baseline identity")
	}
	snapshot.Digest = ""
	data, err := json.Marshal(snapshot)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

// HasCanonical selects the strict reader before any legacy fallback. A broken
// registry with a claimed canonical row remains a strict-path failure.
func HasCanonical(state map[string]any) bool {
	entities, _ := state["entities"].(map[string]any)
	raw, exists := entities["assignments"]
	if !exists {
		return false
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return true
	}
	var rows []map[string]any
	if json.Unmarshal(data, &rows) != nil || rows == nil {
		return true
	}
	runtimeID, _ := state["runtime_id"].(string)
	baseline, _ := state["baseline"].(map[string]any)
	for _, row := range rows {
		if row["runtime_id"] == runtimeID && number(row["baseline_generation"]) == number(baseline["generation"]) {
			return true
		}
		if strings.TrimSpace(textField(row["runtime_id"])) == "" {
			return true
		}
	}
	return false
}

func textField(value any) string { s, _ := value.(string); return s }
func number(value any) int {
	switch v := value.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	case json.Number:
		n, _ := v.Int64()
		return int(n)
	}
	return 0
}
