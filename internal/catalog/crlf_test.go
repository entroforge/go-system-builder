package catalog

import "testing"

func TestFrontmatterAcceptsCRLFWithoutWeakeningDelimiters(t *testing.T) {
	for _, text := range []string{"---\nname: agent\n---\nbody", "---\r\nname: agent\r\n---\r\nbody"} {
		values, body, err := splitFrontmatter(text)
		if err != nil || values["name"] != "agent" || body != "body" {
			t.Fatalf("%q: %+v %q %v", text, values, body, err)
		}
	}
	if _, _, err := splitFrontmatter("---\r\nname: agent"); err == nil {
		t.Fatal("accepted missing closing delimiter")
	}
}
