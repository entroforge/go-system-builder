package projectconventions

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/entroforge/go-system-builder/internal/fileview"
)

func TestOriginalRegistryIgnoresMachineDigestWithoutChangingSource(t *testing.T) {
	for name, old := range map[string]string{"null": "null", "text": `"legacy"`, "number": "42", "object": `{"old":true}`} {
		t.Run(name, func(t *testing.T) {
			root, entry, req := fixture(t)
			data, err := json.Marshal(Registry{SchemaVersion: Version, Entries: []Entry{entry}})
			if err != nil {
				t.Fatal(err)
			}
			var value map[string]any
			if err := json.Unmarshal(data, &value); err != nil {
				t.Fatal(err)
			}
			var oldValue any
			if err := json.Unmarshal([]byte(old), &oldValue); err != nil {
				t.Fatal(err)
			}
			value["entries"].([]any)[0].(map[string]any)["source_refs"].([]any)[0].(map[string]any)["sha256"] = oldValue
			original, _ := json.Marshal(value)
			write(t, root, RegistryPath, string(original))
			got := Resolve(root, fileview.Disk{Root: root}, req)
			if got.RegistryStatus != "present" || len(got.Entries) != 1 || got.Entries[0].Entry.SourceRefs[0].Path != entry.SourceRefs[0].Path {
				t.Fatalf("original source lost: %+v", got)
			}
			after, err := os.ReadFile(filepath.Join(root, RegistryPath))
			if err != nil || !bytes.Equal(after, original) {
				t.Fatalf("original registry changed: %v", err)
			}
		})
	}
}

func TestConventionTypesRetainStandardJSONComposition(t *testing.T) {
	var ref struct {
		Reference
		Extra string `json:"extra"`
	}
	if err := json.Unmarshal([]byte(`{"path":"p","extra":"kept"}`), &ref); err != nil || ref.Extra != "kept" {
		t.Fatalf("reference embedding: %+v, %v", ref, err)
	}
	var src struct {
		Source
		Extra string `json:"extra"`
	}
	if err := json.Unmarshal([]byte(`{"path":"p","extra":"kept"}`), &src); err != nil || src.Extra != "kept" {
		t.Fatalf("source embedding: %+v, %v", src, err)
	}
	var parent struct {
		Ref Reference `json:"ref"`
	}
	decoder := json.NewDecoder(bytes.NewReader([]byte(`{"ref":{"path":"p","unknown":true}}`)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&parent); err == nil {
		t.Fatal("strict parent accepted unknown reference field")
	}
}
