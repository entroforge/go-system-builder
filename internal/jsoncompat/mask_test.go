package jsoncompat

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestMaskMachineFieldsPreservesOriginalStructure(t *testing.T) {
	original := []byte(" \n{\"items\":[{\"sha256\":123,\"document\":{\"sha256\":\"business\"}}, {\"sha256\":{\"nested\":1}}],\"files\":{\"a\\\"b\":{\"sha256\":[1,2]},\"other\":null},\"sha256\":false,\"sha256\":\"second\",\"x\":1,\"x\":2}  {\"trailing\":true}")
	before := append([]byte(nil), original...)
	got, err := MaskMachineFields(original, []string{"sha256"}, []string{"items", "*", "sha256"}, []string{"files", "*", "sha256"})
	if err != nil {
		t.Fatal(err)
	}
	want := " \n{\"items\":[{\"sha256\":null,\"document\":{\"sha256\":\"business\"}}, {\"sha256\":null}],\"files\":{\"a\\\"b\":{\"sha256\":null},\"other\":null},\"sha256\":null,\"sha256\":null,\"x\":1,\"x\":2}  {\"trailing\":true}"
	if string(got) != want {
		t.Fatalf("projection changed other input:\n got %s\nwant %s", got, want)
	}
	if !bytes.Equal(original, before) {
		t.Fatal("original input changed")
	}
}

func TestMaskMachineFieldsKeepsDecoderRules(t *testing.T) {
	type item struct {
		SHA  string `json:"sha256,omitempty"`
		Name string `json:"name"`
	}
	type envelope struct {
		Ref item `json:"ref"`
	}
	for _, test := range []struct {
		name, input string
		wantError   bool
	}{
		{"invalid machine value", `{"ref":{"sha256":[}}`, true},
		{"unknown remains unknown", `{"ref":{"sha256":{},"invented":true}}`, true},
		{"number metadata ignored", `{"ref":{"sha256":3,"name":"ok"}}`, false},
		{"null remains null", `{"ref":null}`, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := MaskMachineFields([]byte(test.input), []string{"ref", "sha256"})
			if err == nil {
				var value envelope
				decoder := json.NewDecoder(bytes.NewReader(got))
				decoder.DisallowUnknownFields()
				err = decoder.Decode(&value)
			}
			if (err != nil) != test.wantError {
				t.Fatalf("error=%v, wantError=%v, projected=%s", err, test.wantError, got)
			}
		})
	}
}

func TestMaskMachineFieldsEscapedKeyAndNoMatch(t *testing.T) {
	data := []byte(`{"\u0073ha256":["old"],"other":{"sha256":"business"},"empty":[]}`)
	got, err := MaskMachineFields(data, []string{"sha256"})
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != `{"\u0073ha256":null,"other":{"sha256":"business"},"empty":[]}` {
		t.Fatalf("wrong escaped key projection: %s", got)
	}
	unchanged, err := MaskMachineFields(data, []string{"missing", "*", "sha256"})
	if err != nil || !bytes.Equal(unchanged, data) || &unchanged[0] != &data[0] {
		t.Fatalf("no-match should preserve input: %q, %v", unchanged, err)
	}
	if _, err := MaskMachineFields(data, []string{}); err == nil || !strings.Contains(err.Error(), "path") {
		t.Fatalf("empty programmer-supplied path should fail: %v", err)
	}
}
