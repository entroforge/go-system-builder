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
	// The frozen workspace binding/preparation path was removed with the old
	// protocol: L4-workflow-convergence.md:312 deletes "旧入口、旧 schema、旧
	// Hook／CLI、旧消费者及兼容适配连同仅为它们存在的测试一并删除", and
	// L3-S6-build.md:316 keeps only the current native dispatch path —
	// "删除 frozen execution 的观察、接收与恢复分支，不以旧协议建立第二条作者路径".
	// `s6 prepare` must therefore not send Main back to the retired commands,
	// and must keep documenting the current native dispatch entry.
	for _, unwanted := range []string{"runtime workspace bind", "runtime workspace prepare"} {
		if strings.Contains(out.String(), unwanted) {
			t.Fatalf("s6 prepare help still advertises the retired %q: %s", unwanted, out.String())
		}
	}
	for _, want := range []string{"native Agent with worktree isolation", "-task", "-role", "-out"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("s6 prepare help missing %q: %s", want, out.String())
		}
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
