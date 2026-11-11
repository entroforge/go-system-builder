package scenario

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestRecoverTransactionPreservesExistingOutputAndRestoresMissingTarget(t *testing.T) {
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
	writeHistoricalTransactionFields(t, directory, json.RawMessage(`{"old":"untrusted"}`))
	notes, err := recoverTransaction(directory)
	if err != nil {
		t.Fatalf("recoverTransaction() error = %v", err)
	}
	if len(notes) != 1 {
		t.Fatalf("expected one advisory about the preserved target and backup, got %v", notes)
	}
	assertFileEquals(t, filepath.Join(directory, casesFile), []byte("new-cases\n"))
	assertFileEquals(t, filepath.Join(directory, coverageFile), oldCoverage)
	assertFileEquals(t, filepath.Join(directory, journal.CasesBackup), oldCases)
	assertFileEquals(t, filepath.Join(directory, journal.CasesTemp), []byte("new-cases\n"))
	if _, err := os.Stat(filepath.Join(directory, transactionFile)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("transaction journal remains, err=%v", err)
	}
	if _, err := recoverTransaction(directory); err != nil {
		t.Fatalf("repeat recovery: %v", err)
	}
	assertFileEquals(t, filepath.Join(directory, journal.CasesBackup), oldCases)
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
	journal, err := os.ReadFile(filepath.Join(directory, transactionFile))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(journal, []byte("fingerprint")) || bytes.Contains(journal, []byte("sha")) {
		t.Fatalf("new journal recorded a digest: %s", journal)
	}
	// A real author may edit the installed output after an interrupted writer.
	if err := os.WriteFile(filepath.Join(directory, casesFile), []byte("author edit\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := recoverTransaction(directory); err != nil {
		t.Fatalf("startup recovery error = %v", err)
	}
	assertFileEquals(t, filepath.Join(directory, casesFile), []byte("author edit\n"))
	assertFileEquals(t, filepath.Join(directory, coverageFile), oldCoverage)
	backups, err := filepath.Glob(filepath.Join(directory, ".scenario-backup-cases.json-*"))
	if err != nil || len(backups) != 1 {
		t.Fatalf("prior cases backup not preserved: %v %v", backups, err)
	}
	assertFileEquals(t, backups[0], oldCases)
	if _, err := os.Stat(filepath.Join(directory, transactionFile)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("transaction journal remains after recovery: %v", err)
	}
}

func TestRollbackErrorsArePropagated(t *testing.T) {
	directory := t.TempDir()
	state := outputSwapState{
		casesPath:      filepath.Join(directory, casesFile),
		casesBackup:    filepath.Join(directory, "cases-backup"),
		coveragePath:   filepath.Join(directory, coverageFile),
		coverageBackup: filepath.Join(directory, "coverage-backup"),
	}
	if err := os.Symlink("somewhere", state.casesBackup); err != nil {
		t.Fatal(err)
	}
	if err := state.restore(); err == nil {
		t.Fatal("restore() swallowed invalid backup error")
	}
}

func TestLegacyJournalDigestValuesDoNotAuthorizeRemoval(t *testing.T) {
	for name, value := range map[string]json.RawMessage{
		"missing": nil, "null": json.RawMessage(`null`),
		"wrong": json.RawMessage(`"not-a-digest"`), "number": json.RawMessage(`7`),
		"object": json.RawMessage(`{"any":true}`),
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			j := transactionJournal{
				Phase:     transactionPhaseOutputsInstalled,
				CasesTemp: ".scenario-cases-temp", CoverageTemp: ".scenario-coverage-temp",
				CasesBackup: ".scenario-cases-backup", CoverageBackup: ".scenario-coverage-backup",
				CasesExisted: true, CoverageExisted: true, CasesInstalled: true, CoverageInstalled: true,
			}
			for path, data := range map[string]string{
				casesFile: "author-cases", coverageFile: "author-coverage",
				j.CasesBackup: "previous-cases", j.CoverageBackup: "previous-coverage",
			} {
				if err := os.WriteFile(filepath.Join(dir, path), []byte(data), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			if err := writeTransactionJournal(dir, j); err != nil {
				t.Fatal(err)
			}
			if value != nil {
				writeHistoricalTransactionFields(t, dir, value)
			}
			notes, err := recoverTransaction(dir)
			if err != nil || len(notes) != 2 {
				t.Fatalf("recovery notes=%v err=%v", notes, err)
			}
			assertFileEquals(t, filepath.Join(dir, casesFile), []byte("author-cases"))
			assertFileEquals(t, filepath.Join(dir, coverageFile), []byte("author-coverage"))
			assertFileEquals(t, filepath.Join(dir, j.CasesBackup), []byte("previous-cases"))
			assertFileEquals(t, filepath.Join(dir, j.CoverageBackup), []byte("previous-coverage"))
		})
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

func writeHistoricalTransactionFields(t *testing.T, directory string, value json.RawMessage) {
	t.Helper()
	path := filepath.Join(directory, transactionFile)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatal(err)
	}
	fields["cases_fingerprint"], fields["coverage_fingerprint"] = value, value
	// An unrelated extension must not become a recovery gate either.
	fields["unrelated_extension"] = json.RawMessage(`{"note":"professional context"}`)
	data, err = json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
}
