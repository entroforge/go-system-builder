package sharedmodel

import "testing"

func TestSectionFenceFastPathSemantics(t *testing.T) {
	for _, tc := range []struct {
		name, content, want string
	}{
		{"no fence", "## 2. Target\nbody\n## Next\nother", "body"},
		{"case sensitive heading", "## target\nbody", ""},
		{"CRLF", "## Target\r\nbody\r\n## Next\r\nother", "body\r"},
		{"closed fence", "```GO\n## Target\nhidden\n```\n## Target\nbody", "body"},
		{"CRLF fence", "```go\r\n## Target\r\nhidden\r\n```\r\n## Target\r\nbody", "body"},
		{"unclosed fence", "```go\n## Target\nbody", "body"},
		{"adjacent fences", "```\n## Target\nhidden\n```\n```\n## Target\nhidden too\n```\n## Target\nbody", "body"},
		{"inline literal", "## Target\ninline ``` marker\nbody", "inline ``` marker\nbody"},
		{"short backticks", "## Target\n``\nbody", "``\nbody"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := Section(tc.content, "Target"); got != tc.want {
				t.Fatalf("Section() = %q, want %q", got, tc.want)
			}
		})
	}
}
