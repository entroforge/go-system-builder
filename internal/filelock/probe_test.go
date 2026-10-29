package filelock

import (
	"bufio"
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestProbeMissingBusyReleasedAndReadOnly(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "absent", "lock")
	if exists, busy, err := Probe(path); err != nil || exists || busy {
		t.Fatal(exists, busy, err)
	}
	if _, err := os.Stat(filepath.Dir(path)); !os.IsNotExist(err) {
		t.Fatal("probe created a directory")
	}
	path = filepath.Join(root, "lock")
	release, err := Acquire(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	before, _ := os.Stat(path)
	dataBefore, _ := os.ReadFile(path)
	if exists, busy, err := Probe(path); err != nil || !exists || !busy {
		t.Fatal(exists, busy, err)
	}
	release()
	if exists, busy, err := Probe(path); err != nil || !exists || busy {
		t.Fatal(exists, busy, err)
	}
	after, _ := os.Stat(path)
	dataAfter, _ := os.ReadFile(path)
	if !os.SameFile(before, after) || !before.ModTime().Equal(after.ModTime()) || !bytes.Equal(dataBefore, dataAfter) {
		t.Fatal("probe changed original lock bytes/inode/time")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Probe(path); err == nil {
		t.Fatal("directory accepted as a lock")
	}
}

func TestProbeChildHolderDeathUsesActualOSLock(t *testing.T) {
	if os.Getenv("GSB_LOCK_PROBE_CHILD") == "1" {
		release, err := Acquire(context.Background(), os.Getenv("GSB_LOCK_PROBE_PATH"))
		if err != nil {
			t.Fatal(err)
		}
		defer release()
		os.Stdout.WriteString("locked\n")
		for {
			time.Sleep(time.Hour)
		}
	}
	path := filepath.Join(t.TempDir(), "lock")
	command := exec.Command(os.Args[0], "-test.run=^TestProbeChildHolderDeathUsesActualOSLock$")
	command.Env = append(os.Environ(), "GSB_LOCK_PROBE_CHILD=1", "GSB_LOCK_PROBE_PATH="+path)
	out, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	defer command.Process.Kill()
	line, err := bufio.NewReader(out).ReadString('\n')
	if err != nil || line != "locked\n" {
		t.Fatal(line, err)
	}
	if exists, busy, err := Probe(path); err != nil || !exists || !busy {
		t.Fatal("child lock not observed", exists, busy, err)
	}
	if err := command.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	command.Wait()
	if exists, busy, err := Probe(path); err != nil || !exists || busy {
		t.Fatal("dead child diagnostic confused with actual lock", exists, busy, err)
	}
}
