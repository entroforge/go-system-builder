package workspace

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestLaunchUsesOnlyChildDirectoryAndRegisteredSession(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake CLI uses POSIX shell")
	}
	b, e, state, ctx := workerFixture(t)
	os.MkdirAll(filepath.Join(b.MainRoot, ".claude/skills"), 0700)
	roles := filepath.Join(b.MainRoot, ".claude/agents")
	os.MkdirAll(roles, 0700)
	os.WriteFile(filepath.Join(roles, "backend-builder.md"), []byte("---\nname: backend-builder\ndescription: Test worker\n---\nImplement assignment."), 0600)
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if err = b.Bootstrap(e, exe); err != nil {
		t.Fatal(err)
	}
	e.BootstrapSHA256, err = BootstrapDigest(e)
	if err != nil {
		t.Fatal(err)
	}
	e.PlatformSessionID, err = NewSessionID()
	if err != nil {
		t.Fatal(err)
	}
	b.Executions[e.AssignmentID] = e
	state["workspace"] = Encode(b)
	saveControlState(t, b.MainRoot, state)
	fake := filepath.Join(t.TempDir(), "claude")
	os.WriteFile(fake, []byte("#!/bin/sh\npwd\nprintf '%s\\n' \"$@\"\nprintf 'parent=%s\\n' \"${CLAUDECODE-unset}\"\n"), 0700)
	t.Setenv("CLAUDECODE", "parent")
	before, _ := os.Getwd()
	mainHead, _ := Git(ctx, b.MainRoot, "rev-parse", "HEAD")
	for _, resume := range []bool{false, true} {
		var out bytes.Buffer
		if err = b.Launch(ctx, e, fake, "backend-builder", "implement", resume, &out, &out); err != nil {
			t.Fatal(err)
		}
		text := out.String()
		flag := "--session-id"
		if resume {
			flag = "--resume"
		}
		if !strings.HasPrefix(text, e.Path+"\n") || !strings.Contains(text, flag+"\n"+e.PlatformSessionID) || !strings.Contains(text, "parent=unset") || strings.Contains(text, "skip-permissions") {
			t.Fatalf("bad launch: %s", text)
		}
	}
	after, _ := os.Getwd()
	head, _ := Git(ctx, b.MainRoot, "rev-parse", "HEAD")
	if after != before || head != mainHead {
		t.Fatal("Main moved")
	}
	owner, err := SessionOwner(ctx, b.MainRoot, e.Path, e.PlatformSessionID)
	if err != nil || owner != e.AgentID {
		t.Fatalf("session owner=%q %v", owner, err)
	}
	if _, err = SessionOwner(ctx, b.MainRoot, e.Path, "unregistered"); err == nil {
		t.Fatal("unknown Worker session accepted")
	}
	owner, err = SessionOwner(ctx, b.MainRoot, b.MainRoot, e.PlatformSessionID)
	if err != nil || owner != "" {
		t.Fatalf("Main impersonated Worker: %q %v", owner, err)
	}
}
