package scenario

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestRecoverTransactionRestoresPriorPairAfterInterruptedRename(t *testing.T) {
	directory := t.TempDir()
	oldCases := []byte("old-cases\n")
	oldCoverage := []byte("old-coverage\n")
	if err := os.WriteFile(filepath.Join(directory, casesFile), oldCases, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, coverageFile), oldCoverage, 0o644); err != nil {
		t.Fatal(err)
	}
	journal := transactionJournal{
		Phase:     transactionPhaseCasesInstalled,
		CasesTemp: ".scenario-cases-temp", CoverageTemp: ".scenario-coverage-temp",
		CasesBackup: ".scenario-cases-backup", CoverageBackup: ".scenario-coverage-backup",
		CasesExisted: true, CoverageExisted: true, CasesInstalled: true,
		CasesFingerprint: fingerprint([]byte("new-cases\n")), CoverageFingerprint: fingerprint([]byte("new-coverage\n")),
	}
	if err := os.WriteFile(filepath.Join(directory, journal.CasesBackup), oldCases, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, journal.CoverageBackup), oldCoverage, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, casesFile), []byte("new-cases\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(directory, coverageFile)); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, journal.CasesTemp), []byte("new-cases\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writeTransactionJournal(directory, journal); err != nil {
		t.Fatal(err)
	}
	if err := recoverTransaction(directory); err != nil {
		t.Fatalf("recoverTransaction() error = %v", err)
	}
	assertFileEquals(t, filepath.Join(directory, casesFile), oldCases)
	assertFileEquals(t, filepath.Join(directory, coverageFile), oldCoverage)
	for _, name := range []string{transactionFile, journal.CasesBackup, journal.CoverageBackup, journal.CasesTemp} {
		if _, err := os.Stat(filepath.Join(directory, name)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("transaction artifact %s remains, err=%v", name, err)
		}
	}
}

func TestAtomicOutputRecoveryAfterFaultInjection(t *testing.T) {
	directory := t.TempDir()
	oldCases := []byte("old-cases\n")
	oldCoverage := []byte("old-coverage\n")
	if err := os.WriteFile(filepath.Join(directory, casesFile), oldCases, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, coverageFile), oldCoverage, 0o644); err != nil {
		t.Fatal(err)
	}
	previousHook := atomicFaultHook
	defer func() { atomicFaultHook = previousHook }()
	atomicFaultHook = func(point atomicFaultPoint) {
		if point == faultAfterCasesInstalledJournal {
			panic("simulated process interruption")
		}
	}
	func() {
		defer func() {
			if recovered := recover(); recovered == nil {
				t.Error("fault injection did not interrupt the transaction")
			}
		}()
		if err := writeOutputsAtomically(directory, []byte("new-cases\n"), []byte("new-coverage\n")); err != nil {
			t.Errorf("fault-injected write returned before interruption: %v", err)
		}
	}()
	atomicFaultHook = nil
	if err := recoverTransaction(directory); err != nil {
		t.Fatalf("startup recovery error = %v", err)
	}
	assertFileEquals(t, filepath.Join(directory, casesFile), oldCases)
	assertFileEquals(t, filepath.Join(directory, coverageFile), oldCoverage)
	if _, err := os.Stat(filepath.Join(directory, transactionFile)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("transaction journal remains after recovery: %v", err)
	}
}

func TestRollbackErrorsArePropagated(t *testing.T) {
	state := outputSwapState{
		casesPath:        filepath.Join(t.TempDir(), casesFile),
		casesBackup:      filepath.Join(t.TempDir(), "missing-cases-backup"),
		casesInstalled:   true,
		casesFingerprint: fingerprint([]byte("new-cases\n")),
	}
	if err := os.WriteFile(state.casesPath, []byte("old-cases\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := state.restore(); err == nil {
		t.Fatal("restore() swallowed missing backup error")
	}
}

func assertFileEquals(t *testing.T, path string, want []byte) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if string(got) != string(want) {
		t.Fatalf("%s = %q, want %q", path, got, want)
	}
}
