//go:build !windows

package fileview

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestReadFilesCancellationJoinsAllStartedGitProcesses(t *testing.T) {
	view := pinnedReadFixture(t, 16)
	bin := t.TempDir()
	pids := filepath.Join(bin, "started")
	// exec leaves no helper shell behind: cancellation must reap each exact
	// process that the batch started, before ReadFiles returns.
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte("#!/bin/sh\necho $$ >> \"$FILEVIEW_TEST_GIT_PIDS\"\nexec sleep 30\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FILEVIEW_TEST_GIT_PIDS", pids)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	ctx, cancel := context.WithCancel(WithReadCache(context.Background()))
	defer cancel()
	paths := make([]string, 16)
	for i := range paths {
		paths[i] = fmt.Sprintf("source-%02d", i)
	}
	done := make(chan error, 1)
	go func() {
		done <- ReadFiles(view.WithContext(ctx), paths, func(_ int, _ []byte, err error) error { return err })
	}()
	deadline := time.Now().Add(3 * time.Second)
	var started []string
	for time.Now().Before(deadline) {
		data, _ := os.ReadFile(pids)
		started = strings.Fields(string(data))
		if len(started) >= 4 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if len(started) != 4 {
		cancel()
		<-done
		t.Fatalf("expected exactly four bounded workers, started %d", len(started))
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled batch: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("batch did not join canceled Git reads")
	}
	for _, pidText := range started {
		pid, err := strconv.Atoi(pidText)
		if err != nil {
			t.Fatal(err)
		}
		if err := syscall.Kill(pid, 0); !errors.Is(err, syscall.ESRCH) {
			t.Fatalf("Git process %d survived batch cancellation: %v", pid, err)
		}
	}
}
