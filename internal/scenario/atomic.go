package scenario

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const transactionFile = ".scenario-transaction.json"

type transactionPhase string

const (
	transactionPhasePrepared         transactionPhase = "prepared"
	transactionPhaseCasesBackedUp    transactionPhase = "cases_backed_up"
	transactionPhaseBackedUp         transactionPhase = "backed_up"
	transactionPhaseCasesInstalled   transactionPhase = "cases_installed"
	transactionPhaseOutputsInstalled transactionPhase = "outputs_installed"
)

type transactionJournal struct {
	Phase             transactionPhase `json:"phase"`
	CasesTemp         string           `json:"cases_temp"`
	CoverageTemp      string           `json:"coverage_temp"`
	CasesBackup       string           `json:"cases_backup"`
	CoverageBackup    string           `json:"coverage_backup"`
	CasesExisted      bool             `json:"cases_existed"`
	CoverageExisted   bool             `json:"coverage_existed"`
	CasesInstalled    bool             `json:"cases_installed"`
	CoverageInstalled bool             `json:"coverage_installed"`
}

type atomicFaultPoint string

const faultAfterCasesInstalledJournal atomicFaultPoint = "after_cases_installed_journal"

// atomicFaultHook is intentionally private and nil in production. Tests use
// it to model a process disappearing between durable transaction steps.
var atomicFaultHook func(atomicFaultPoint)

// writeOutputsAtomically stages both outputs and records every state change in
// a small recoverable journal. This is crash-recoverable replacement, not a
// claim of filesystem-level atomicity across two independent renames.
func writeOutputsAtomically(directory string, cases, coverage []byte) error {
	if _, err := recoverTransaction(directory); err != nil {
		return fmt.Errorf("recover previous output transaction: %w", err)
	}
	if err := ensureWritableTarget(filepath.Join(directory, casesFile)); err != nil {
		return err
	}
	if err := ensureWritableTarget(filepath.Join(directory, coverageFile)); err != nil {
		return err
	}
	casesTemp, err := stageFile(directory, ".scenario-cases-", cases)
	if err != nil {
		return err
	}
	coverageTemp, err := stageFile(directory, ".scenario-coverage-", coverage)
	if err != nil {
		return errors.Join(err, removeRegularFile(casesTemp))
	}
	state := outputSwapState{
		directory: directory,
		casesPath: filepath.Join(directory, casesFile), coveragePath: filepath.Join(directory, coverageFile),
		casesTemp: casesTemp, coverageTemp: coverageTemp,
		casesBackup:    filepath.Join(directory, ".scenario-backup-cases.json-"+filepath.Base(casesTemp)),
		coverageBackup: filepath.Join(directory, ".scenario-backup-coverage.json-"+filepath.Base(coverageTemp)),
	}
	state.casesExisted, err = targetExists(state.casesPath)
	if err != nil {
		return state.fail(err)
	}
	state.coverageExisted, err = targetExists(state.coveragePath)
	if err != nil {
		return state.fail(err)
	}
	if err := ensureAbsentTransactionArtifact(state.casesBackup); err != nil {
		return state.fail(err)
	}
	if err := ensureAbsentTransactionArtifact(state.coverageBackup); err != nil {
		return state.fail(err)
	}
	journal := state.journal(transactionPhasePrepared)
	if err := writeTransactionJournal(directory, journal); err != nil {
		return state.fail(err)
	}
	if state.casesExisted {
		if _, err := moveIfExists(state.casesPath, state.casesBackup); err != nil {
			return state.fail(fmt.Errorf("stage existing cases.json: %w", err))
		}
		if err := writeTransactionJournal(directory, state.journal(transactionPhaseCasesBackedUp)); err != nil {
			return state.fail(err)
		}
	}
	if state.coverageExisted {
		if _, err := moveIfExists(state.coveragePath, state.coverageBackup); err != nil {
			return state.fail(fmt.Errorf("stage existing scenario-coverage.json: %w", err))
		}
	}
	if err := writeTransactionJournal(directory, state.journal(transactionPhaseBackedUp)); err != nil {
		return state.fail(err)
	}
	if err := os.Rename(state.casesTemp, state.casesPath); err != nil {
		return state.fail(fmt.Errorf("install cases.json: %w", err))
	}
	state.casesInstalled = true
	if err := writeTransactionJournal(directory, state.journal(transactionPhaseCasesInstalled)); err != nil {
		return state.fail(err)
	}
	triggerAtomicFault(faultAfterCasesInstalledJournal)
	if err := os.Rename(state.coverageTemp, state.coveragePath); err != nil {
		return state.fail(fmt.Errorf("install scenario-coverage.json: %w", err))
	}
	state.coverageInstalled = true
	if err := writeTransactionJournal(directory, state.journal(transactionPhaseOutputsInstalled)); err != nil {
		return state.fail(err)
	}
	if err := state.finish(); err != nil {
		return fmt.Errorf("finalize output transaction: %w", err)
	}
	return nil
}

type outputSwapState struct {
	directory                         string
	casesPath, coveragePath           string
	casesTemp, coverageTemp           string
	casesBackup, coverageBackup       string
	casesExisted, coverageExisted     bool
	casesInstalled, coverageInstalled bool
}

func (state outputSwapState) journal(phase transactionPhase) transactionJournal {
	return transactionJournal{
		Phase: phase, CasesTemp: filepath.Base(state.casesTemp), CoverageTemp: filepath.Base(state.coverageTemp),
		CasesBackup: filepath.Base(state.casesBackup), CoverageBackup: filepath.Base(state.coverageBackup),
		CasesExisted: state.casesExisted, CoverageExisted: state.coverageExisted,
		CasesInstalled: state.casesInstalled, CoverageInstalled: state.coverageInstalled,
	}
}

func (state outputSwapState) fail(primary error) error {
	rollbackErr := state.restore()
	if rollbackErr != nil {
		// Keep the recorded transaction when recovery itself cannot proceed.
		return errors.Join(primary, fmt.Errorf("rollback output transaction: %w", rollbackErr))
	}
	if journalErr := removeTransactionJournal(state.directory); journalErr != nil {
		return errors.Join(primary, fmt.Errorf("retire output transaction: %w", journalErr))
	}
	return primary
}

func (state outputSwapState) restore() error {
	var restoreErr error
	// Existing targets may have been edited since an interruption. Preserve
	// both them and any backup rather than guessing which bytes are ours.
	if err := restoreBackupIfTargetMissing(state.casesBackup, state.casesPath); err != nil {
		restoreErr = errors.Join(restoreErr, err)
	}
	if err := restoreBackupIfTargetMissing(state.coverageBackup, state.coveragePath); err != nil {
		restoreErr = errors.Join(restoreErr, err)
	}
	return restoreErr
}

func (state outputSwapState) finish() error {
	var cleanupErr error
	if err := removeRegularFile(state.casesTemp); err != nil {
		cleanupErr = errors.Join(cleanupErr, err)
	}
	if err := removeRegularFile(state.coverageTemp); err != nil {
		cleanupErr = errors.Join(cleanupErr, err)
	}
	if err := removeRegularFile(state.casesBackup); err != nil {
		cleanupErr = errors.Join(cleanupErr, err)
	}
	if err := removeRegularFile(state.coverageBackup); err != nil {
		cleanupErr = errors.Join(cleanupErr, err)
	}
	if cleanupErr != nil {
		return cleanupErr
	}
	return removeTransactionJournal(state.directory)
}

func recoverTransaction(directory string) ([]string, error) {
	journalPath := filepath.Join(directory, transactionFile)
	info, err := os.Lstat(journalPath)
	if errors.Is(err, os.ErrNotExist) {
		// An unreferenced file may contain a user's changes; its name alone
		// does not authorize the framework to remove it.
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("inspect transaction journal: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("transaction journal is not a regular file")
	}
	data, err := readRegularFile(journalPath)
	if err != nil {
		return nil, fmt.Errorf("read transaction journal: %w", err)
	}
	var journal transactionJournal
	// Unknown historical transaction properties do not authorize recovery.
	// Decode the complete JSON while using only the original transaction paths
	// and steps; json.Unmarshal also rejects trailing or malformed input.
	if err := json.Unmarshal(data, &journal); err != nil {
		return nil, fmt.Errorf("decode transaction journal: %w", err)
	}
	state, err := stateFromJournal(directory, journal)
	if err != nil {
		return nil, err
	}
	var notes []string
	for _, pair := range [][2]string{{state.casesPath, state.casesBackup}, {state.coveragePath, state.coverageBackup}} {
		target, targetErr := targetExists(pair[0])
		backup, backupErr := targetExists(pair[1])
		if targetErr != nil || backupErr != nil {
			return nil, errors.Join(targetErr, backupErr)
		}
		if target && backup {
			notes = append(notes, fmt.Sprintf("Preserved current output %s and prior backup %s; their interrupted transaction history is unknown.", pair[0], pair[1]))
		}
	}
	if err := state.restore(); err != nil {
		return nil, fmt.Errorf("rollback recovered output transaction: %w", err)
	}
	if err := removeTransactionJournal(directory); err != nil {
		return nil, fmt.Errorf("remove recovered transaction journal: %w", err)
	}
	return notes, nil
}

func stateFromJournal(directory string, journal transactionJournal) (outputSwapState, error) {
	if journal.Phase != transactionPhasePrepared && journal.Phase != transactionPhaseCasesBackedUp && journal.Phase != transactionPhaseBackedUp && journal.Phase != transactionPhaseCasesInstalled && journal.Phase != transactionPhaseOutputsInstalled {
		return outputSwapState{}, fmt.Errorf("unknown transaction phase %q", journal.Phase)
	}
	paths := []struct {
		name string
	}{
		{journal.CasesTemp}, {journal.CoverageTemp},
		{journal.CasesBackup}, {journal.CoverageBackup},
	}
	for _, item := range paths {
		if err := validateTransactionArtifactName(item.name); err != nil {
			return outputSwapState{}, err
		}
		if err := ensureWritableTarget(filepath.Join(directory, item.name)); err != nil {
			return outputSwapState{}, err
		}
	}
	state := outputSwapState{
		directory: directory,
		casesPath: filepath.Join(directory, casesFile), coveragePath: filepath.Join(directory, coverageFile),
		casesTemp: filepath.Join(directory, journal.CasesTemp), coverageTemp: filepath.Join(directory, journal.CoverageTemp),
		casesBackup: filepath.Join(directory, journal.CasesBackup), coverageBackup: filepath.Join(directory, journal.CoverageBackup),
		casesExisted: journal.CasesExisted, coverageExisted: journal.CoverageExisted,
		casesInstalled: journal.CasesInstalled, coverageInstalled: journal.CoverageInstalled,
	}
	if err := ensureWritableTarget(state.casesPath); err != nil {
		return outputSwapState{}, err
	}
	if err := ensureWritableTarget(state.coveragePath); err != nil {
		return outputSwapState{}, err
	}
	backupCases, err := targetExists(state.casesBackup)
	if err != nil {
		return outputSwapState{}, err
	}
	backupCoverage, err := targetExists(state.coverageBackup)
	if err != nil {
		return outputSwapState{}, err
	}
	state.casesExisted = state.casesExisted || backupCases
	state.coverageExisted = state.coverageExisted || backupCoverage
	return state, nil
}

func writeTransactionJournal(directory string, journal transactionJournal) error {
	data, err := marshalStable(journal)
	if err != nil {
		return fmt.Errorf("encode transaction journal: %w", err)
	}
	path := filepath.Join(directory, transactionFile)
	if err := ensureWritableTarget(path); err != nil {
		return err
	}
	temp, err := stageFile(directory, ".scenario-journal-", data)
	if err != nil {
		return err
	}
	if err := os.Rename(temp, path); err != nil {
		cleanupErr := removeRegularFile(temp)
		return errors.Join(fmt.Errorf("install transaction journal: %w", err), cleanupErr)
	}
	return nil
}

func removeTransactionJournal(directory string) error {
	return removeRegularFile(filepath.Join(directory, transactionFile))
}

func triggerAtomicFault(point atomicFaultPoint) {
	if atomicFaultHook != nil {
		atomicFaultHook(point)
	}
}

func stageFile(directory, pattern string, data []byte) (string, error) {
	file, err := os.CreateTemp(directory, pattern)
	if err != nil {
		return "", fmt.Errorf("create staged output: %w", err)
	}
	path := file.Name()
	_, writeErr := file.Write(data)
	syncErr := error(nil)
	if writeErr == nil {
		syncErr = file.Sync()
	}
	closeErr := file.Close()
	if writeErr != nil || syncErr != nil || closeErr != nil {
		return path, errors.Join(writeErr, syncErr, closeErr)
	}
	return path, nil
}

func moveIfExists(source, destination string) (bool, error) {
	info, err := os.Lstat(source)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return false, fmt.Errorf("refuse non-regular output %s", source)
	}
	if err := os.Rename(source, destination); err != nil {
		return false, err
	}
	return true, nil
}

func ensureWritableTarget(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect writable target %s: %w", path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("refuse symlink writable target %s", path)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("writable target %s is not regular", path)
	}
	return nil
}

func ensureAbsentTransactionArtifact(path string) error {
	_, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	return fmt.Errorf("transaction artifact already exists %s", path)
}

func targetExists(path string) (bool, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return false, fmt.Errorf("transaction target %s is not regular", path)
	}
	return true, nil
}

func restoreBackupIfTargetMissing(backup, target string) error {
	backupExists, err := targetExists(backup)
	if err != nil {
		return err
	}
	if !backupExists {
		return nil
	}
	if exists, err := targetExists(target); err != nil {
		return err
	} else if exists {
		return nil
	}
	if err := os.Rename(backup, target); err != nil {
		return fmt.Errorf("restore backup %s: %w", backup, err)
	}
	return nil
}

func removeRegularFile(path string) error {
	if path == "" {
		return nil
	}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return fmt.Errorf("refuse to remove non-regular transaction artifact %s", path)
	}
	return os.Remove(path)
}

func readRegularFile(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("transaction artifact %s is not regular", path)
	}
	return os.ReadFile(path)
}

func validateTransactionArtifactName(name string) error {
	if name == "" || filepath.Base(name) != name || strings.Contains(name, "/") || strings.Contains(name, `\`) || !strings.HasPrefix(name, ".scenario-") {
		return fmt.Errorf("invalid transaction artifact name %q", name)
	}
	return nil
}
