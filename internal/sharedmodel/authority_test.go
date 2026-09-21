package sharedmodel

import (
	"reflect"
	"sort"
	"testing"
)

func TestResourceIdentityUsesCompilerSemantics(t *testing.T) {
	for _, tc := range []struct {
		name, schema string
		want         []string
	}{
		{"instance annotations", `{"$id":"https://example.test/root","default":{"$id":"https://example.test/data"},"const":{"$id":"https://example.test/data"},"examples":[{"$id":"https://example.test/data"}],"properties":{"$id":{"type":"string"}}}`, []string{"https://example.test/root"}},
		{"draft7 ref sibling", `{"$schema":"http://json-schema.org/draft-07/schema#","$ref":"#/definitions/value","$id":"https://example.test/ignored","definitions":{"value":{"type":"string"}}}`, nil},
		{"draft4 identifier", `{"$schema":"http://json-schema.org/draft-04/schema#","id":"https://example.test/root","$id":"https://example.test/ignored","definitions":{"anchored":{"id":"#node","type":"string"}}}`, []string{"https://example.test/root"}},
		{"relative nested resource", `{"$id":"https://example.test/models/root","$defs":{"child":{"$id":"../child"}}}`, []string{"https://example.test/child", "https://example.test/models/root"}},
		{"nested schema without resource", `{"$id":"https://example.test/root","$defs":{"child":{"$schema":"http://json-schema.org/draft-04/schema#","$defs":{"value":{"$id":"https://example.test/value"}}}}}`, []string{"https://example.test/root", "https://example.test/value"}},
		{"large JSON number", `{"$id":"https://example.test/root","minimum":1e999}`, []string{"https://example.test/root"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ids, err := schemaResourceIDs(t.TempDir(), "model.json", []byte(tc.schema))
			if err != nil {
				t.Fatal(err)
			}
			var got []string
			for id := range ids {
				got = append(got, id)
			}
			sort.Strings(got)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("resource identities = %v; want %v", got, tc.want)
			}
		})
	}
}
