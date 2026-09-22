package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestHookBudgetNeverPublishesPartialAllow(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX fake Hook process")
	}
	script := filepath.Join(t.TempDir(), "hook")
	os.WriteFile(script, []byte("#!/bin/sh\nprintf '{\"decision\":\"allow\"}'\nsleep 30\n"), 0700)
	for _, tool := range []string{"Write", "Bash", "Read"} {
		t.Run(tool, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
			defer cancel()
			var out, errout bytes.Buffer
			start := time.Now()
			code := runBudgetedHook(ctx, script, nil, strings.NewReader(`{"hook_event_name":"PreToolUse","tool_name":"`+tool+`"}`), &out, &errout)
			want := 2
			if tool == "Read" {
				want = 0
			}
			if code != want || out.Len() != 0 || !strings.Contains(errout.String(), "internal budget") {
				t.Fatalf("code=%d stdout=%s stderr=%s", code, &out, &errout)
			}
			if time.Since(start) > 2*time.Second {
				t.Fatal("deadline failed to bound process")
			}
		})
	}
}
