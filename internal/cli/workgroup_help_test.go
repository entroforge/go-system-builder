package cli_test

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/cli"
)

func TestWorkgroupHelpExposesOriginalFlagsWithoutRuntime(t *testing.T) {
	for _, help := range []string{"--help", "-h", "help"} {
		t.Run(help, func(t *testing.T) {
			root := t.TempDir()
			var out, errout bytes.Buffer
			code := cli.Run([]string{"runtime", "register-workgroup", "--root", root, help}, nil, &out, &errout)
			if code != 0 || errout.Len() != 0 {
				t.Fatalf("help failed: code=%d stdout=%s stderr=%s", code, out.String(), errout.String())
			}
			for _, flag := range []string{"-manifest", "-task-id", "-task", "-revise-assignment", "-expected-record-ref"} {
				if !strings.Contains(out.String(), flag) {
					t.Fatalf("help hides original flag %s: %s", flag, out.String())
				}
			}
			entries, err := os.ReadDir(root)
			if err != nil || len(entries) != 0 {
				t.Fatalf("help created Runtime or artifacts: %v %v", entries, err)
			}
		})
	}
}

func TestS6HelpNamesSupportedStatusAndFlags(t *testing.T) {
	for _, args := range [][]string{{"s6", "--help"}, {"s6", "status", "--help"}} {
		var out, errout bytes.Buffer
		if code := cli.Run(args, nil, &out, &errout); code != 0 || errout.Len() != 0 {
			t.Fatalf("%v: code=%d stdout=%s stderr=%s", args, code, out.String(), errout.String())
		}
		for _, want := range []string{"s6 status", "capacity", "advice"} {
			if !strings.Contains(out.String(), want) {
				t.Fatalf("%v help missing %q: %s", args, want, out.String())
			}
		}
		if strings.Contains(out.String(), "scaffolding") || strings.Contains(out.String(), "TASK generation") {
			t.Fatalf("help advertises an unsupported command: %s", out.String())
		}
	}
	var out, errout bytes.Buffer
	if code := cli.Run([]string{"s6", "prepare", "--help"}, nil, &out, &errout); code != 0 || errout.Len() != 0 {
		t.Fatalf("s6 prepare help: code=%d stdout=%s stderr=%s", code, out.String(), errout.String())
	}
	if !strings.Contains(out.String(), "runtime workspace bind --root <authority-root>") {
		t.Fatalf("s6 prepare help omits the existing Main workspace bind command: %s", out.String())
	}
}

func TestLifecycleLeafHelpExposesOriginalFlagsWithoutRuntime(t *testing.T) {
	for _, tc := range []struct{ args, flags []string }{
		{[]string{"runtime", "pause"}, []string{"-root", "-approved-by", "-reason"}},
		{[]string{"runtime", "resume"}, []string{"-root", "-approved-by"}},
		{[]string{"req", "amend"}, []string{"-root", "-approved-by", "-req"}},
	} {
		for _, help := range []string{"--help", "-h", "help"} {
			t.Run(strings.Join(tc.args, " ")+" "+help, func(t *testing.T) {
				root := t.TempDir()
				args := append(append([]string{}, tc.args...), "--root", root, help)
				var out, errout bytes.Buffer
				if code := cli.Run(args, nil, &out, &errout); code != 0 || errout.Len() != 0 {
					t.Fatalf("help failed code=%d stdout=%s stderr=%s", code, out.String(), errout.String())
				}
				for _, flag := range tc.flags {
					if !strings.Contains(out.String(), flag) {
						t.Fatalf("help hides %s: %s", flag, out.String())
					}
				}
				entries, err := os.ReadDir(root)
				if err != nil || len(entries) != 0 {
					t.Fatalf("help created artifacts: %v %v", entries, err)
				}
			})
		}
	}
}
