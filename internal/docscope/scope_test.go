package docscope

import "testing"

func TestExplicitOwnership(t *testing.T) {
	for _, tc := range []struct {
		name, text string
		want       bool
	}{
		{"current", "> Source REQ refs: REQ-053\n## Body\nREQ-052", true},
		{"foreign with current body", "> Source REQ refs: REQ-052\n## Body\nREQ-053", false},
		{"prefix collision", "> Source REQ refs: REQ-0530", false},
		{"shared", "> Source REQ refs: REQ-052, REQ-053", true},
		{"CRLF path", "> 关联需求：`docs/requirements/REQ-053.md`\r\n", true},
		{"legacy unlabelled", "> Status: complete", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := Belongs([]byte(tc.text), "REQ-053"); got != tc.want {
				t.Fatalf("got %v", got)
			}
		})
	}
}
