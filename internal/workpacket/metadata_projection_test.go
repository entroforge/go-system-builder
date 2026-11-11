package workpacket

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestPacketReferenceDecodingKeepsOriginalRules(t *testing.T) {
	var embedded struct {
		Reference
		Extra string `json:"extra"`
	}
	if err := json.Unmarshal([]byte(`{"id":"REQ","extra":"kept"}`), &embedded); err != nil || embedded.ID != "REQ" || embedded.Extra != "kept" {
		t.Fatalf("anonymous DTO embedding changed: %+v %v", embedded, err)
	}
	for _, old := range []string{`null`, `12`, `{"old":true}`, `"legacy"`} {
		var got Reference
		if err := json.Unmarshal([]byte(`{"id":"REQ","sha256":`+old+`}`), &got); err != nil || got.ID != "REQ" {
			t.Fatalf("historical field %s: %+v %v", old, got, err)
		}
	}
	data := []byte(`{"id":"one","id":"two","sha256":{}} {"tail":true}`)
	if strings.Count(string(data), `"id"`) != 2 {
		t.Fatalf("test input lost duplicate fields: %s", data)
	}
	var first Reference
	if err := json.NewDecoder(bytes.NewReader(data)).Decode(&first); err != nil || first.ID != "two" {
		t.Fatalf("original streaming decoder semantics changed: %+v %v", first, err)
	}
	if err := json.Unmarshal(data, &first); err == nil {
		t.Fatal("original json.Unmarshal trailing-input rejection changed")
	}
	if err := json.Unmarshal([]byte(`{"id":"REQ","sha256":3,"unknown":true}`), &first); err != nil || first.ID != "REQ" {
		t.Fatalf("unrelated fields narrowed reference decoding: %+v %v", first, err)
	}
}
