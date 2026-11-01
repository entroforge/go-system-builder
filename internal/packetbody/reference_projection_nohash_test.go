package packetbody

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestReferenceKeepsOriginalPathWithUnrelatedFields(t *testing.T) {
	row := map[string]any{"storage_format": StorageFormat, "body_ref": map[string]any{"format": BodyFormat, "path": Directory + "p/example.json", "size_bytes": map[string]any{"unrelated": true}, "note": map[string]any{"arbitrary": "professional context"}}}
	before, err := json.Marshal(row)
	if err != nil {
		t.Fatal(err)
	}
	ref, present, err := ReferenceFromIndex(row)
	if err != nil || !present || ref.Path != Directory+"p/example.json" {
		t.Fatalf("original reference: %+v %t %v", ref, present, err)
	}
	after, err := json.Marshal(row)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("reference read changed original index")
	}
	type envelope struct {
		Reference
		Extra string `json:"extra"`
	}
	var out envelope
	if err := json.Unmarshal([]byte(`{"format":"packet-json-v1","path":"p","size_bytes":1,"extra":"kept"}`), &out); err != nil || out.Extra != "kept" {
		t.Fatalf("outer field lost: %+v %v", out, err)
	}
}
