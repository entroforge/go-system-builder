package checkexec

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/schema"
)

func TestProfileFileRefUsesActualPathWithoutDigest(t *testing.T) {
	ref := FileRef{Path: "fixtures/actual.json"}
	data, err := json.Marshal(ref)
	if err != nil || strings.Contains(string(data), "sha256") {
		t.Fatalf("source ref: %s %v", data, err)
	}
	var actual FileRef
	if err := json.Unmarshal(data, &actual); err != nil || actual.Path != ref.Path {
		t.Fatalf("source ref round trip: %+v %v", actual, err)
	}
}

func TestProfileSchemaUsesActualDeclaredCommandAndSource(t *testing.T) {
	profile := testProfile()
	profile.Fixtures = []FileRef{{Path: "fixtures/actual.json"}}
	data, err := json.Marshal(profile)
	if err != nil {
		t.Fatal(err)
	}
	if err := schema.NewEmbeddedValidator().ValidateBytes("check-profile.schema.json", data); err != nil {
		t.Fatal(err)
	}
	if err := ValidateProfiles([]Profile{profile}, []string{"true"}, "main"); err != nil {
		t.Fatal(err)
	}
	if err := ValidateProfiles([]Profile{profile}, []string{"false"}, "main"); err == nil {
		t.Fatal("different command accepted")
	}
}
