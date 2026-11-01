package evidence

import "testing"

func TestParsePathReferenceLocatesOriginalPathWithoutDigestRequirement(t *testing.T) {
	for _, ref := range []string{"path:observation.txt", "path:observation.txt#sha256=anything", "path:observation.txt#sha256=", "path:observation.txt#ordinary-section", "path:observation.txt#", "path:observation.txt#hash-is-product-text"} {
		path, err := ParsePathReference(ref)
		if err != nil || path != "observation.txt" {
			t.Fatalf("original reference %q: path=%q err=%v", ref, path, err)
		}
	}
}
