package cli_test

import (
	"bytes"
	"github.com/entroforge/go-system-builder/internal/cli"
	"strings"
	"testing"
)

func TestInvestigationHelpExposesActualLeafFlagsWithoutRuntime(t *testing.T) {
	for _, tc := range []struct {
		verb []string
		flag string
	}{
		{[]string{"dispatch"}, "-agent-definition"},
		{[]string{"hypothesis", "register"}, "-evidence"},
		{[]string{"hypothesis", "result"}, "-evidence"},
		{[]string{"route"}, "-no-competing-hypothesis"},
		{[]string{"contract", "approve"}, "-approval-evidence-id"},
	} {
		t.Run(strings.Join(tc.verb, "/"), func(t *testing.T) {
			args := append([]string{"runtime", "investigation"}, tc.verb...)
			args = append(args, "--root", t.TempDir(), "--help")
			var out, errout bytes.Buffer
			code := cli.Run(args, strings.NewReader(""), &out, &errout)
			if code != 0 || !strings.Contains(out.String(), tc.flag) || errout.Len() != 0 {
				t.Fatalf("code=%d out=%s err=%s", code, out.String(), errout.String())
			}
		})
	}
}
